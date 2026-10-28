package app

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/travisennis/cake-repl/internal/cake"
	"github.com/travisennis/cake-repl/internal/suggest"
	"github.com/travisennis/cake-repl/internal/ui"
)

const (
	// suggestionTimeout bounds one suggester run. A suggestion is a small model
	// call made after every completed turn; a hung child must not keep a request
	// identity or a child process alive for the life of the REPL.
	suggestionTimeout = 60 * time.Second
	// suggestionTeardownTimeout bounds how long an exit that never reaches the
	// Update loop waits for a canceled suggester to be reaped. The suggester's
	// own WaitDelay is three seconds; this leaves room for scheduling.
	suggestionTeardownTimeout = 5 * time.Second
	// maxSuggestionBytes bounds the ghost text placed in the composer. A prompt
	// suggestion longer than this is not a useful one-line hint.
	maxSuggestionBytes = 4 * 1024
)

// suggestionDoneMsg delivers the terminal state of one suggester subprocess.
// req is the request identity; a result whose identity no longer matches is
// superseded and must not touch the composer.
type suggestionDoneMsg struct {
	req       uint64
	sessionID string
	res       suggest.Result
}

// suggestionTurnEligible reports whether a finished run is a completed turn
// that may carry a suggestion. Cancellations and failures keep the previous
// composer state; the next completed turn re-evaluates. A completion record
// marked is_error is a task failure and is rejected by canSuggest.
func (m Model) suggestionTurnEligible(res cake.Result) bool {
	return !res.Canceled && res.Err == nil && res.ExitCode == 0 && m.sawComplete
}

// canSuggest reports whether the REPL may start a suggestion request now. It is
// deliberately stricter than "a turn just ended": the request starts only with
// an empty composer, an idle program, and a pinned session that has a completed
// turn behind it. A draft always wins over a suggestion.
//
// Compaction is intentionally not part of the gate: the suggester reads the
// session through cake's read-only replay stream, which tolerates a concurrent
// append, and the two model calls overlap. A result that arrives while the
// compacting gate still blocks submission is held for the composer. See ADR
// 031.
func (m Model) canSuggest() bool {
	if !m.cfg.Suggestions.Enabled || m.cfg.NoSession {
		return false
	}
	if m.running || m.hydrating || m.replayPending || m.replayRun != nil ||
		m.browserOpen || m.exitAfter || m.newSessionPending {
		return false
	}
	if m.suggesting || m.suggestionDone != nil || m.suggestionText != "" ||
		m.pendingSuggestion != "" || m.input.Value() != "" {
		return false
	}
	if m.session.LastComplete == nil || m.session.LastComplete.IsError {
		return false
	}
	mode, sessionID := m.session.RunOptions()
	return mode == cake.RunResume && sessionID != ""
}

// maybeStartSuggestion starts one suggestion request when the REPL is eligible.
// It returns a nil command and an unchanged model when no request is possible,
// so callers can use it on every post-turn path without branching first.
func (m Model) maybeStartSuggestion() (Model, tea.Cmd) {
	if !m.canSuggest() {
		return m, nil
	}
	_, sessionID := m.session.RunOptions()

	ctx, cancel := context.WithTimeout(context.Background(), suggestionTimeout)
	done := make(chan struct{})
	m.suggestionSeq++
	req := m.suggestionSeq
	m.suggestionReq = req
	m.suggestionCancel = cancel
	m.suggestionDone = done
	m.suggesting = true
	opts := suggest.Options{
		Bin:             m.cfg.Suggestions.Bin,
		Cwd:             m.cfg.Cwd,
		SessionID:       sessionID,
		CakeBin:         m.cfg.CakeBin,
		Model:           firstNonEmpty(m.cfg.Suggestions.Model, m.cfg.Model),
		Profile:         firstNonEmpty(m.cfg.Suggestions.Profile, m.cfg.Profile),
		ReasoningEffort: m.cfg.Suggestions.ReasoningEffort,
		DebugLog:        m.cfg.DebugLog,
	}
	return m, func() tea.Msg {
		res := suggest.Suggest(ctx, opts)
		// Close before returning the message: an exit that never reaches Update
		// (SIGINT/SIGTERM) can wait on the child here, even if the program's own
		// message channel is already gone.
		close(done)
		return suggestionDoneMsg{req: req, sessionID: sessionID, res: res}
	}
}

// finishSuggestion applies one suggester result. A stale request, a canceled or
// failed request, a no-op, or a document for another session never changes the
// composer. A usable result is shown only if the REPL is still idle, the input
// is still empty, and the session pin is still the one the request named.
func (m Model) finishSuggestion(msg suggestionDoneMsg) Model {
	// The child is reaped by the time any result arrives, including a result for
	// a request that typing or a session change already invalidated. Settle the
	// drain state before deciding whether the document may be used.
	m.suggestionDone = nil
	if msg.req == 0 || msg.req != m.suggestionReq {
		return m
	}
	m.suggesting = false
	m.suggestionReq = 0
	if m.suggestionCancel != nil {
		m.suggestionCancel()
		m.suggestionCancel = nil
	}

	if msg.res.Canceled {
		m.logDebug("suggestion: canceled")
		return m
	}
	if msg.res.Err != nil || msg.res.ExitCode != 0 || msg.res.Document == nil {
		m.warnSuggestion(suggestionFailureText(msg.res, m.cfg.OutputLimit))
		m.syncViewport()
		return m
	}
	doc := *msg.res.Document
	if doc.Suggested == nil {
		// The required field is absent or null. That is a malformed document, not
		// the explicit no-op the contract permits, so it must not pass silently.
		m.warnSuggestion(suggestionMalformedText)
		m.syncViewport()
		return m
	}
	if !*doc.Suggested {
		m.logDebug("suggestion: no-op")
		return m
	}
	if doc.SessionID != msg.sessionID {
		// The suggester read a different session than the request named. This is
		// a contract failure, not a suggestion for the session the user is in.
		m.logDebug("suggestion: session mismatch")
		return m
	}
	text, ok := normalizeSuggestion(doc.Suggestion)
	if !ok {
		return m
	}
	if !m.canSuggest() {
		// The user typed, started a run, opened the browser, or changed the
		// session while the child was finishing. The result belongs to the state
		// it started in.
		return m
	}
	if _, pinned := m.session.RunOptions(); pinned != msg.sessionID {
		// The composer is idle and empty, but the pin is no longer the session
		// the suggester read. Identity invalidation should already have dropped
		// this result; never apply across a session boundary regardless.
		m.logDebug("suggestion: session pin changed")
		return m
	}
	if m.compacting {
		// The composer is gated until the compactor is reaped. Hold the usable
		// result so it can appear the moment the gate opens instead of starting
		// a second metered request afterward. See ADR 031.
		m.pendingSuggestion = text
		m.pendingSuggestionSession = msg.sessionID
		return m
	}

	m.showSuggestion(text)
	return m
}

// showSuggestion puts one usable, already-normalized suggestion in the
// composer as ghost text.
func (m *Model) showSuggestion(text string) {
	m.suggestionText = text
	m.input.Placeholder = text
	m.layout()
	m.contentChanged = true
}

// applyPendingSuggestion promotes a suggestion held during compaction once the
// composer is usable again. It re-checks the same identity and composer rules as
// a live result; anything that changed while the compactor ran drops the held
// text instead.
func (m *Model) applyPendingSuggestion() {
	if m.pendingSuggestion == "" {
		return
	}
	text := m.pendingSuggestion
	sessionID := m.pendingSuggestionSession
	m.pendingSuggestion = ""
	m.pendingSuggestionSession = ""
	if !m.canSuggest() {
		return
	}
	if _, pinned := m.session.RunOptions(); pinned != sessionID {
		m.logDebug("suggestion: held session pin changed")
		return
	}
	m.showSuggestion(text)
}

// warnSuggestion surfaces a broken suggester once per REPL process. The feature
// runs after every completed turn, so a repeated warning row would bury the
// conversation; the debug log still records every lifecycle result.
func (m *Model) warnSuggestion(text string) {
	if m.suggestionWarned {
		return
	}
	m.suggestionWarned = true
	m.appendItem(ui.Item{Kind: ui.KindWarning, Text: text})
}

// clearSuggestion removes the visible ghost and cancels any in-flight request.
// It is called for every input action that supersedes a suggestion: typing,
// history recall, completion, submit, a new session, a resume, and quit.
func (m *Model) clearSuggestion() {
	m.suggestionText = ""
	m.pendingSuggestion = ""
	m.pendingSuggestionSession = ""
	if m.input.Placeholder != defaultInputPlaceholder {
		m.input.Placeholder = defaultInputPlaceholder
		m.contentChanged = true
	}
	m.cancelSuggestion()
}

// cancelSuggestion stops an in-flight request and invalidates its identity. The
// child may take a moment to exit, but its late result is ignored. The drain
// channel stays set until that result arrives so an exit cannot race the child;
// canSuggest refuses to start another request until it is reaped.
func (m *Model) cancelSuggestion() {
	if m.suggestionCancel != nil {
		m.suggestionCancel()
		m.suggestionCancel = nil
	}
	m.suggesting = false
	m.suggestionReq = 0
}

// suggestionActive reports whether a ghost suggestion is currently shown.
func (m Model) suggestionActive() bool {
	return m.suggestionText != ""
}

// normalizeSuggestion turns the suggester's document text into one bounded,
// control-free composer line. Session content is model-generated and must not
// be able to emit terminal controls through the placeholder, and a suggestion
// is a single prompt-like hint rather than a multi-line document.
func normalizeSuggestion(s string) (string, bool) {
	s = strings.Join(strings.Fields(ui.Sanitize(s)), " ")
	if s == "" {
		return "", false
	}
	if len(s) > maxSuggestionBytes {
		cut := maxSuggestionBytes
		for cut > 0 && !utf8.RuneStart(s[cut]) {
			cut--
		}
		s = strings.TrimSpace(s[:cut]) + "…"
	}
	return s, true
}

// suggestionMalformedText is the one diagnostic for a syntactically valid
// document that omits a required field. It shares the failure warning budget.
const suggestionMalformedText = "prompt suggestion unavailable: malformed document (missing suggested)"

// suggestionFailureText renders the one diagnostic for a failed suggester run.
// The suggester's stderr is already bounded; the render path sanitizes it.
func suggestionFailureText(res suggest.Result, outputLimit int) string {
	text := "prompt suggestion unavailable"
	switch {
	case res.Err != nil:
		text += ": " + ui.TruncateOutput(res.Err.Error(), outputLimit)
	case res.Stderr != "":
		text += ": " + ui.TruncateOutput(res.Stderr, outputLimit)
	case res.ExitCode >= 0:
		text += fmt.Sprintf(" (exit code %d)", res.ExitCode)
	}
	return text
}
