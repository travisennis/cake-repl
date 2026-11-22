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

const compactionTriggerHeadroomPercent = 5

const compactionWindowFallbackPercent = 75

const compactionStartText = "context compaction started · Ctrl+C cancels"

type compactionTrigger struct {
	absolute bool
	tokens   int
	percent  int
}

type compactionDoneMsg struct {
	req       uint64
	sessionID string
	res       compact.Result
}

func (m Model) shouldCompactAfter() bool {
	if !m.cfg.Compaction.Enabled || m.cfg.NoSession || m.runBusy() ||
		m.compactionBusy() || m.exiting() || !m.completedTurn() {
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

func (m Model) startCompaction() (Model, tea.Cmd) {
	mode, sessionID := m.session.RunOptions()
	if mode != cake.RunResume || sessionID == "" || m.runBusy() ||
		m.compactionBusy() || m.exiting() {
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
	done := make(chan struct{})
	m.trackChild("compaction", cancel, done)
	m.compactionSeq++
	key := operationKey{request: m.compactionSeq}
	m.compaction.begin(key, cancel, done)
	m.compactionStart = time.Now()
	m.compactionElapsed = 0
	req := key.request
	opts := compact.Options{
		Bin:                       m.cfg.Compaction.Bin,
		Cwd:                       m.cfg.Cwd,
		SessionID:                 sessionID,
		CakeBin:                   m.cfg.CakeBin,
		KeepTokens:                m.cfg.Compaction.KeepTokens,
		SummarizerModel:           firstNonEmpty(m.cfg.Compaction.Model, m.effectiveModel()),
		SummarizerProfile:         firstNonEmpty(m.cfg.Compaction.Profile, m.effectiveProfile()),
		SummarizerReasoningEffort: m.cfg.Compaction.ReasoningEffort,
		DebugLog:                  m.cfg.DebugLog,
	}
	if trigger.absolute {
		opts.ThresholdTokens = trigger.tokens
	} else {
		opts.ThresholdPercent = m.cfg.Compaction.ThresholdPercent
	}
	run := func() tea.Msg {
		res := compact.Compact(ctx, opts)
		close(done)
		return compactionDoneMsg{req: req, sessionID: sessionID, res: res}
	}
	m.compactionNotice = m.appendNotice(ui.Item{Kind: ui.KindInfo, Text: compactionStartText})
	m.syncViewport()
	return m, run
}

func (m Model) finishCompaction(msg compactionDoneMsg) (Model, tea.Cmd) {
	if !m.compaction.settle(operationKey{request: msg.req}) {
		m.suggestion.dropHeld()
		return m.maybeQuit()
	}
	m.compactionStart = time.Time{}
	m.compactionElapsed = 0

	mode, sessionID := m.session.RunOptions()
	if mode != cake.RunResume || sessionID != msg.sessionID {
		m.removeCompactionStart()
		m.suggestion.dropHeld()
		if m.exiting() {
			return m.maybeQuit()
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
	if m.exiting() {
		return m.maybeQuit()
	}
	m.applyPendingSuggestion()
	return m, m.titleCmd()
}

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
	m.session.OnCompactionEstimate(doc.AfterTokens, doc.Window)
	m.finishCompactionNotice(ui.Item{
		Kind: ui.KindInfo,
		Text: compactionMarker(doc.BeforeTokens, doc.AfterTokens, doc.RetainedCount),
	})
}

func (m *Model) removeCompactionStart() bool {
	id := m.compactionNotice
	m.compactionNotice = 0
	return m.removeNotice(id)
}

func (m *Model) finishCompactionNotice(it ui.Item) {
	m.removeCompactionStart()
	m.appendItem(it)
}

func (m *Model) skipCompactionNotice(it ui.Item) {
	if m.removeCompactionStart() {
		m.appendItem(it)
	}
}

func compactionSkippedText(reason string, outputLimit int) string {
	text := "context compaction skipped"
	if reason = strings.TrimSpace(reason); reason != "" {
		text += ": " + ui.TrimForDisplay(reason, outputLimit)
	}
	return text
}

func (m Model) compactionBusy() bool {
	return m.compaction.active() || m.compaction.draining()
}

func (m *Model) refreshCompactionElapsed(now time.Time) {
	if !m.compactionBusy() || m.compactionStart.IsZero() {
		return
	}
	elapsed := now.Sub(m.compactionStart)
	if elapsed < 0 {
		elapsed = 0
	}
	m.compactionElapsed = elapsed.Truncate(time.Second)
}

func elapsedText(d time.Duration) string {
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

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func compactionFailureText(res compact.Result, outputLimit int) string {
	text := "context compaction failed"
	switch {
	case res.Err != nil:
		text += ": " + res.Err.Error()
	case res.Stderr != "":
		text += ": " + ui.TrimForDisplay(res.Stderr, outputLimit)
	case res.ExitCode >= 0:
		text += fmt.Sprintf(" (exit code %d)", res.ExitCode)
	}
	return text
}
