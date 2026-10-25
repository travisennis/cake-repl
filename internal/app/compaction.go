package app

import (
	"context"
	"fmt"

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
	// The title picks up the compacting marker in the same transition. The
	// spinner needs no new tick: finishRun runs while the finished turn's tick
	// is still pending, and the handler keeps that chain alive while compacting.
	return m, tea.Batch(run, m.titleCmd())
}

// finishCompaction applies one compactor result. In-place success keeps the
// session pin unchanged, appends one shared marker, and seeds the status context
// from the compactor's estimate. A no-op is silent; a failure, cancellation, or
// non-in-place result keeps the session and status figure and warns.
func (m Model) finishCompaction(msg compactionDoneMsg) (Model, tea.Cmd) {
	if msg.req != m.compactionReq {
		return m, nil
	}
	m.compacting = false
	m.compactionCancel = nil

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
		// the session the REPL left.
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
		m.appendItem(ui.Item{Kind: ui.KindWarning, Text: "context compaction canceled"})
	default:
		m.appendItem(ui.Item{Kind: ui.KindWarning, Text: compactionFailureText(msg.res, m.cfg.OutputLimit)})
	}

	m.syncViewport()
	if m.exitAfter {
		return m.quit()
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
		return
	}
	if !doc.InPlace || doc.SessionID != sessionID {
		m.appendItem(ui.Item{
			Kind: ui.KindWarning,
			Text: "context compaction did not append in place; keeping session " + ui.ShortID(sessionID),
		})
		return
	}
	// The compactor's document carries the window it used; when it does not, the
	// session's last known window is kept. The estimate is marked so the status
	// line renders it with "~".
	m.session.OnCompactionEstimate(doc.AfterTokens, doc.Window)
	m.appendItem(ui.Item{
		Kind: ui.KindInfo,
		Text: compactionMarker(doc.BeforeTokens, doc.AfterTokens, doc.RetainedCount),
	})
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
