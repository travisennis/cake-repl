package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/travisennis/cake-repl/internal/cake"
	"github.com/travisennis/cake-repl/internal/compact"
	"github.com/travisennis/cake-repl/internal/ui"
)

// compactionTriggerHeadroomPercent is how far below the compactor's own
// threshold the REPL starts it in percent mode. A complete turn_usage measures
// the prompt the model saw, not the response and tool results that follow it;
// those items land in the session before the compactor reads it. Triggering a
// little early lets the compactor's fuller session estimate cross its real
// threshold, while the compactor stays the authority and returns
// compacted:false when it does not.
const compactionTriggerHeadroomPercent = 5

// compactionWindowFallbackPercent caps an absolute trigger when the model
// window is smaller than the configured quality ceiling. It matches the
// effective line of the default percent mode (80 - 5), so a small window still
// leaves headroom for the response and tool results and the trigger can never
// sit above the window.
const compactionWindowFallbackPercent = 75

// compactionStartPrefix marks the live compaction start notice in the timeline.
// The completion path finds that row and resolves it into the outcome, so a
// started row never keeps reading as pending; completed and replayed markers use
// different wording and never carry it. See ADR 026.
const compactionStartPrefix = "context compaction started"

// compactionStartText is the one timeline notice shown before the compactor is
// launched. The Ctrl+C hint is repeated in the composer footer so it stays
// visible after the timeline scrolls.
const compactionStartText = compactionStartPrefix + " · Ctrl+C cancels"

// compactionTrigger is one resolved trigger line: either an absolute token
// count (absolute mode) or a window percentage (the legacy fallback).
type compactionTrigger struct {
	absolute bool
	tokens   int
	percent  int
}

// compactionDoneMsg delivers the terminal state of one compactor subprocess.
// req is the request identity; a result whose identity no longer matches is
// superseded and must not touch the session or timeline.
type compactionDoneMsg struct {
	req       uint64
	sessionID string
	res       compact.Result
}

// shouldCompactAfter reports whether a finished turn leaves the current session
// over the configured trigger. It only compacts a run that delivered a
// completion record and exited cleanly: a canceled or failed run keeps whatever
// context it had, and the next successful turn re-evaluates.
func (m Model) shouldCompactAfter(res cake.Result) bool {
	if !m.cfg.Compaction.Enabled || m.cfg.NoSession || m.compacting || m.exitAfter {
		return false
	}
	if res.Canceled || res.Err != nil || res.ExitCode != 0 || !m.sawComplete {
		return false
	}
	// A task_complete that reports an error is a finished turn, but compacting a
	// failed task spends quota on a transcript the user is likely still fixing.
	// The next successful turn re-evaluates.
	if m.session.LastComplete == nil || m.session.LastComplete.IsError {
		return false
	}
	mode, sessionID := m.session.RunOptions()
	if mode != cake.RunResume || sessionID == "" {
		return false
	}
	c := m.session.Context
	if c == nil || c.Tokens <= 0 || c.Window <= 0 {
		return false
	}
	trigger, ok := m.resolveCompactionTrigger(c.Window)
	if !ok {
		return false
	}
	if trigger.absolute {
		return c.Tokens >= trigger.tokens
	}
	return c.Tokens*100 >= c.Window*trigger.percent
}

// resolveCompactionTrigger resolves the configured trigger for a positive
// window. Absolute mode requires both absolute fields and a trigger below the
// ceiling; an incomplete absolute config disables the trigger rather than
// silently falling back to a percentage. When the window is smaller than the
// ceiling, the effective trigger is capped at compactionWindowFallbackPercent
// of the window, so it never exceeds the window. Percent mode preserves the
// five-point headroom and is the fallback when both absolute fields are unset.
func (m Model) resolveCompactionTrigger(window int) (compactionTrigger, bool) {
	c := m.cfg.Compaction
	if c.MaxContextTokens != 0 || c.TriggerTokens != 0 {
		if window <= 0 || c.MaxContextTokens <= 0 || c.TriggerTokens <= 0 ||
			c.TriggerTokens >= c.MaxContextTokens {
			return compactionTrigger{}, false
		}
		tokens := c.TriggerTokens
		if window < c.MaxContextTokens {
			safe := window * compactionWindowFallbackPercent / 100
			if safe < tokens {
				tokens = safe
			}
		}
		if tokens <= 0 {
			return compactionTrigger{}, false
		}
		return compactionTrigger{absolute: true, tokens: tokens}, true
	}
	percent := m.compactionTriggerPercent()
	if percent <= 0 {
		return compactionTrigger{}, false
	}
	return compactionTrigger{percent: percent}, true
}

// compactionTriggerPercent is the configured threshold less the measurement
// headroom. An unconfigured or invalid threshold disables the trigger rather
// than guessing; main resolves the config defaults before the model is built.
func (m Model) compactionTriggerPercent() int {
	p := m.cfg.Compaction.ThresholdPercent
	if p < 1 || p > 100 {
		return 0
	}
	if p > compactionTriggerHeadroomPercent {
		return p - compactionTriggerHeadroomPercent
	}
	return 1
}

// startCompaction launches one in-place compaction as a Bubble Tea command. The
// model-owned context is canceled by Ctrl+C, quit, and Ctrl+N; the gate stays
// closed until the result is reaped. It returns a nil command, leaving state
// untouched, when there is no pinned session or no resolvable trigger.
func (m Model) startCompaction() (Model, tea.Cmd) {
	mode, sessionID := m.session.RunOptions()
	if mode != cake.RunResume || sessionID == "" {
		return m, nil
	}

	window := 0
	if m.session.Context != nil {
		window = m.session.Context.Window
	}
	trigger, ok := m.resolveCompactionTrigger(window)
	if !ok {
		return m, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	m.compactionSeq++
	m.compactionReq = m.compactionSeq
	m.compactionCancel = cancel
	m.compacting = true
	m.compactionStart = time.Now()
	m.compactionElapsed = 0
	req := m.compactionReq
	opts := compact.Options{
		Bin:               m.cfg.Compaction.Bin,
		Cwd:               m.cfg.Cwd,
		SessionID:         sessionID,
		CakeBin:           m.cfg.CakeBin,
		KeepTokens:        m.cfg.Compaction.KeepTokens,
		SummarizerModel:   m.cfg.Model,
		SummarizerProfile: m.cfg.Profile,
		DebugLog:          m.cfg.DebugLog,
	}
	if trigger.absolute {
		opts.ThresholdTokens = trigger.tokens
	} else {
		opts.ThresholdPercent = m.cfg.Compaction.ThresholdPercent
	}
	run := func() tea.Msg {
		return compactionDoneMsg{req: req, sessionID: sessionID, res: compact.Compact(ctx, opts)}
	}
	// The start notice is synced here, not left for the next event: the finished
	// turn's Update has already pushed its completion row, and no spinner tick
	// pushes a viewport payload. Without this sync a long compaction would show
	// a busy status line and composer but no timeline start until the result
	// lands. See ADR 026.
	m.appendItem(ui.Item{Kind: ui.KindInfo, Text: compactionStartText})
	m.syncViewport()
	// The title picks up the compacting marker in the same transition. The
	// spinner needs no new tick: finishRun runs while the finished turn's tick
	// is still pending, and the handler keeps that chain alive while compacting.
	return m, tea.Batch(run, m.titleCmd())
}

// finishCompaction applies one compactor result. In-place success keeps the
// session pin unchanged, resolves the start notice into one shared marker, and
// seeds the status context from the compactor's estimate. A no-op resolves the
// start notice with an explicit skip notice; a failure, cancellation, or
// non-in-place result keeps the session and status figure and warns.
func (m Model) finishCompaction(msg compactionDoneMsg) (Model, tea.Cmd) {
	if msg.req != m.compactionReq {
		return m, nil
	}
	m.compacting = false
	m.compactionCancel = nil
	m.compactionStart = time.Time{}
	m.compactionElapsed = 0

	// Ctrl+N moved to a new session while the canceled child was draining; its
	// result belongs before that boundary.
	if m.newSessionPending {
		m.newSessionPending = false
		if m.exitAfter {
			return m.quit()
		}
		m.syncViewport()
		return m, m.titleCmd()
	}

	mode, sessionID := m.session.RunOptions()
	if mode != cake.RunResume || sessionID != msg.sessionID {
		// The target changed while the child was draining; its result belongs to
		// the session the REPL left. Drop the pending start row so it cannot
		// keep reading as in progress after the operation is over.
		m.removeCompactionStart()
		if m.exitAfter {
			return m.quit()
		}
		m.syncViewport()
		return m, m.titleCmd()
	}

	switch {
	case msg.res.Document != nil && msg.res.ExitCode == 0:
		m.applyCompactionDocument(msg.sessionID, *msg.res.Document)
	case msg.res.Canceled:
		m.finishCompactionNotice(ui.Item{Kind: ui.KindWarning, Text: "context compaction canceled"})
	default:
		m.finishCompactionNotice(ui.Item{Kind: ui.KindWarning, Text: compactionFailureText(msg.res, m.cfg.OutputLimit)})
	}

	m.syncViewport()
	if m.exitAfter {
		return m.quit()
	}
	// The compactor is reaped, so the session is stable and the suggestion can
	// read it. A new session, a retarget, or an exit returned above.
	if next, cmd := m.maybeStartSuggestion(); cmd != nil {
		return next, tea.Batch(cmd, next.titleCmd())
	}
	return m, m.titleCmd()
}

// applyCompactionDocument turns one success document into the live notice and
// estimated context. A document that did not append in place cannot be accepted:
// its session id is not the one the next prompt is pinned to, so the REPL keeps
// the current session and reports the mismatch instead of retargeting.
func (m *Model) applyCompactionDocument(sessionID string, doc compact.Document) {
	if !doc.Compacted {
		m.logDebug("compaction: no-op: " + doc.Reason)
		m.skipCompactionNotice(ui.Item{
			Kind: ui.KindInfo,
			Text: compactionSkippedText(doc.Reason, m.cfg.OutputLimit),
		})
		return
	}
	if !doc.InPlace || doc.SessionID != sessionID {
		m.finishCompactionNotice(ui.Item{
			Kind: ui.KindWarning,
			Text: "context compaction did not append in place; keeping session " + ui.ShortID(sessionID),
		})
		return
	}
	// The compactor's document carries the window it used; when it does not, the
	// session's last known window is kept. The estimate is marked so the status
	// line renders it with "~".
	m.session.OnCompactionEstimate(doc.AfterTokens, doc.Window)
	m.finishCompactionNotice(ui.Item{
		Kind: ui.KindInfo,
		Text: compactionMarker(doc.BeforeTokens, doc.AfterTokens, doc.RetainedCount),
	})
}

// removeCompactionStart removes the live start notice and reports whether one
// was still on the timeline. Ctrl+N, /clear, or the timeline cap can drop it
// before the result is reaped, so the completion path cannot assume it is there.
func (m *Model) removeCompactionStart() bool {
	for i := len(m.items) - 1; i >= 0; i-- {
		if m.items[i].Kind == ui.KindInfo && strings.HasPrefix(m.items[i].Text, compactionStartPrefix) {
			m.removeItemAt(i)
			return true
		}
	}
	return false
}

// finishCompactionNotice resolves the live start notice into one outcome row.
// The row is appended at the end instead of updated in place so an action the
// user attempted while the gate was closed (a rejected submit or /new) stays
// before the outcome. With no start notice left, the outcome is still appended: a
// success, failure, or cancellation must remain visible even after /clear.
func (m *Model) finishCompactionNotice(it ui.Item) {
	m.removeCompactionStart()
	m.appendItem(it)
}

// skipCompactionNotice resolves the start notice when the compactor declined.
// A no-op is expected behavior, so it stays silent when the row it would have
// resolved is already gone; otherwise the row becomes the explicit skip.
func (m *Model) skipCompactionNotice(it ui.Item) {
	if m.removeCompactionStart() {
		m.appendItem(it)
	}
}

// compactionSkippedText names a no-op result. The compactor re-checks every
// eligible turn, so a decline is routine; the reason is the compactor's short
// explanation, bounded here and sanitized by the render boundary.
func compactionSkippedText(reason string, outputLimit int) string {
	text := "context compaction skipped"
	if reason = strings.TrimSpace(reason); reason != "" {
		text += ": " + ui.TruncateOutput(reason, outputLimit)
	}
	return text
}

// refreshCompactionElapsed records the whole-second age of the running
// compactor. The spinner tick is the clock the REPL already runs while a task or
// compaction is in flight, so this adds no second timer and stops with the
// spinner chain when the REPL goes idle.
func (m *Model) refreshCompactionElapsed(now time.Time) {
	if !m.compacting || m.compactionStart.IsZero() {
		return
	}
	elapsed := now.Sub(m.compactionStart)
	if elapsed < 0 {
		elapsed = 0
	}
	m.compactionElapsed = elapsed.Truncate(time.Second)
}

// compactionElapsedText formats the running compactor's whole-second age for
// the one-row status line and composer title. Whole seconds keep it short on a
// narrow terminal and change at most once per second, however often the
// spinner frame renders.
func compactionElapsedText(d time.Duration) string {
	secs := int64(d / time.Second)
	if secs < 0 {
		secs = 0
	}
	switch {
	case secs < 60:
		return fmt.Sprintf("%ds", secs)
	case secs < 3600:
		return fmt.Sprintf("%dm%02ds", secs/60, secs%60)
	default:
		return fmt.Sprintf("%dh%02dm%02ds", secs/3600, (secs/60)%60, secs%60)
	}
}

// compactionFailureText renders the one diagnostic for a failed compactor run.
// The compactor's stderr is already bounded; the render path truncates and
// sanitizes it for display.
func compactionFailureText(res compact.Result, outputLimit int) string {
	text := "context compaction failed"
	switch {
	case res.Err != nil:
		text += ": " + res.Err.Error()
	case res.Stderr != "":
		text += ": " + ui.TruncateOutput(res.Stderr, outputLimit)
	case res.ExitCode >= 0:
		text += fmt.Sprintf(" (exit code %d)", res.ExitCode)
	}
	return text
}
