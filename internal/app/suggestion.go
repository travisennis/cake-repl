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
	// maxSuggestionBytes bounds the ghost text placed in the composer. A prompt
	// suggestion longer than this is not a useful one-line hint.
	maxSuggestionBytes = 4 * 1024
)

// suggestionDoneMsg delivers the terminal state of one suggester subprocess.
// key is the request identity; a result whose identity no longer matches is
// superseded and must not touch the composer.
type suggestionDoneMsg struct {
	key operationKey
	res suggest.Result
}

// suggestionPhase is the one answer for the suggestion's lifecycle and
// presentation. A request phase and a result phase are mutually exclusive:
// only one request may run, its usable result is either shown or held, and a
// held result is never shown at the same time. The phase is derived from the
// state fields so a request and its presentation cannot disagree.
type suggestionPhase uint8

const (
	suggestionDisabled suggestionPhase = iota
	suggestionIdle
	suggestionInFlight
	suggestionDraining
	suggestionHeld
	suggestionShown
)

// suggestionHold is a usable result waiting for compaction to release the
// composer. key preserves the request and session identity so the held text
// cannot cross a session boundary.
type suggestionHold struct {
	text string
	key  operationKey
}

// suggestionState owns the single after-turn suggestion request and its
// presentation. request is the child lifecycle; ghost, held, and filledText are
// pure presentation. warned keeps a broken suggester to one visible warning per
// REPL process.
type suggestionState struct {
	request pendingOperation
	seq     uint64
	ghost   string
	held    suggestionHold
	// filledText is the exact composer value copied from ghost by fillSuggestion.
	// While the trimmed composer still matches it, submit bypasses slash-command
	// parsing; an edit changes the comparison and restores normal parsing.
	filledText string
	// hydrated marks a clean startup replay that ended at a completed, non-errored
	// turn. It is suggestion-only eligibility: it must not write the live run
	// outcome or enable between-turn compaction. clearSuggestion drops it at any
	// draft or session boundary.
	hydrated bool
	warned   bool
}

// phase returns the one suggestion lifecycle state. Disabled is a config
// state, not a request state: it short-circuits everything else so canSuggest
// never has to re-derive the same answer from individual flags.
func (s suggestionState) phase(enabled bool) suggestionPhase {
	if !enabled {
		return suggestionDisabled
	}
	switch {
	case s.request.active():
		return suggestionInFlight
	case s.request.draining():
		return suggestionDraining
	case s.held.text != "":
		return suggestionHeld
	case s.ghost != "":
		return suggestionShown
	default:
		return suggestionIdle
	}
}

// begin allocates a generation-scoped request key and enters the in-flight
// phase. The caller has already established that the suggestion phase is idle,
// so ghost and held are empty; start must not write them here or it would clear
// the presentation without updating the composer placeholder.
func (s *suggestionState) begin(generation sessionGeneration, sessionID string, cancel context.CancelFunc, done <-chan struct{}) operationKey {
	s.seq++
	key := operationKey{generation: generation, request: s.seq, sessionID: sessionID}
	s.request.begin(key, cancel, done)
	return key
}

func (s *suggestionState) settle(key operationKey) bool {
	return s.request.settle(key)
}

func (s *suggestionState) invalidate() {
	s.request.invalidate()
}

func (s *suggestionState) doneChannel() <-chan struct{} {
	return s.request.doneChannel()
}

// hold stores a usable result while compaction gates the composer. The caller
// has already established that no ghost is shown, so the composer placeholder
// still names the default rather than a result that is now held.
func (s *suggestionState) hold(text string, key operationKey) {
	s.held = suggestionHold{text: text, key: key}
}

// takeHeld removes and returns the held result, leaving the phase idle before
// the caller re-checks whether it may be shown.
func (s *suggestionState) takeHeld() suggestionHold {
	hold := s.held
	s.held = suggestionHold{}
	return hold
}

func (s *suggestionState) dropHeld() {
	s.held = suggestionHold{}
}

// suggestionPhase scopes the state to this process's resolved config. A
// `-no-session` REPL has no pinned target for the suggester to read, so it is
// disabled just like an unset `[suggestions] enabled`.
func (m Model) suggestionPhase() suggestionPhase {
	return m.suggestion.phase(m.cfg.Suggestions.Enabled && !m.cfg.NoSession)
}

// suggestionBusy reports whether a suggester child still owns a resource. A
// held or shown result has no child and is not busy.
func (m Model) suggestionBusy() bool {
	p := m.suggestionPhase()
	return p == suggestionInFlight || p == suggestionDraining
}

// canSuggest reports whether the REPL may start a suggestion request now. It is
// deliberately stricter than "a turn just ended": the request starts only with
// an empty composer, an idle program, and a pinned session that has a completed
// turn behind it. A clean startup hydration that ended at a completed turn is
// the other source of that evidence. A draft always wins over a suggestion.
//
// Compaction is intentionally not part of the gate: the suggester reads the
// session through cake's read-only replay stream, which tolerates a concurrent
// append, and the two model calls overlap. A result that arrives while the
// compacting gate still blocks submission is held for the composer. See ADR
// 031.
func (m Model) canSuggest() bool {
	if m.suggestionPhase() != suggestionIdle {
		return false
	}
	if !m.admitSuggestion().allowed {
		return false
	}
	if m.input.Value() != "" {
		return false
	}
	if !m.completedTurn() && !m.suggestion.hydrated {
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
	m.trackChild("suggestion", cancel, done)
	key := m.suggestion.begin(m.session.Generation(), sessionID, cancel, done)
	opts := suggest.Options{
		Bin:             m.cfg.Suggestions.Bin,
		Cwd:             m.cfg.Cwd,
		SessionID:       sessionID,
		CakeBin:         m.cfg.CakeBin,
		Model:           firstNonEmpty(m.cfg.Suggestions.Model, m.turn.Model),
		Profile:         firstNonEmpty(m.cfg.Suggestions.Profile, m.turn.Profile),
		ReasoningEffort: m.cfg.Suggestions.ReasoningEffort,
		DebugLog:        m.cfg.DebugLog,
	}
	return m, func() tea.Msg {
		res := suggest.Suggest(ctx, opts)
		// Close before returning the message: an exit that never reaches Update
		// (SIGINT/SIGTERM) can wait on the child here, even if the program's own
		// message channel is already gone.
		close(done)
		return suggestionDoneMsg{key: key, res: res}
	}
}

// finishSuggestion applies one suggester result. A stale request, a canceled or
// failed request, a no-op, or a document for another session never changes the
// composer. A usable result is shown only if the REPL is still idle, the input
// is still empty, and the session pin is still the one the request named.
func (m Model) finishSuggestion(msg suggestionDoneMsg) Model {
	// The child is reaped by the time any result arrives, including a result for
	// a request that typing or a session change already invalidated. Settle the
	// drain state before deciding whether the document may be used; an
	// invalidated request only closes its drain and never applies.
	if !m.suggestion.settle(msg.key) {
		return m
	}
	if msg.key.generation != m.session.Generation() {
		// The request was reaped after the target changed. The generation owns
		// suppression here; the phase still had to settle so a canceled child can
		// finish draining.
		m.logDebug("suggestion: stale generation")
		return m
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
	if doc.SessionID != msg.key.sessionID {
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
	if _, pinned := m.session.RunOptions(); pinned != msg.key.sessionID {
		// The composer is idle and empty, but the pin is no longer the session
		// the suggester read. Identity invalidation should already have dropped
		// this result; never apply across a session boundary regardless.
		m.logDebug("suggestion: session pin changed")
		return m
	}
	if m.compactionBusy() {
		// The composer is gated until the compactor is reaped. Hold the usable
		// result so it can appear the moment the gate opens instead of starting
		// a second metered request afterward. See ADR 031.
		m.suggestion.hold(text, msg.key)
		return m
	}

	m.showSuggestion(text)
	return m
}

// showSuggestion puts one usable, already-normalized suggestion in the
// composer as ghost text. It is the only transition into the shown phase.
func (m *Model) showSuggestion(text string) {
	m.suggestion.dropHeld()
	m.writeSuggestionGhost(text)
	m.layout()
	m.contentChanged = true
}

// writeSuggestionGhost is the one runtime writer for both the suggestion's
// ghost text and the composer placeholder. Keeping the pair in one function
// makes it impossible for the visible placeholder to name stale text.
func (m *Model) writeSuggestionGhost(text string) {
	m.suggestion.ghost = text
	placeholder := defaultInputPlaceholder
	if text != "" {
		placeholder = text
	}
	if m.input.Placeholder != placeholder {
		m.input.Placeholder = placeholder
		m.contentChanged = true
	}
}

// applyPendingSuggestion promotes a suggestion held during compaction once the
// composer is usable again. It re-checks the same identity and composer rules as
// a live result; anything that changed while the compactor ran drops the held
// text instead.
func (m *Model) applyPendingSuggestion() {
	hold := m.suggestion.takeHeld()
	if hold.text == "" {
		return
	}
	if hold.key.generation != m.session.Generation() {
		m.logDebug("suggestion: held generation changed")
		return
	}
	if _, pinned := m.session.RunOptions(); pinned != hold.key.sessionID {
		m.logDebug("suggestion: held session pin changed")
		return
	}
	if !m.canSuggest() {
		return
	}
	m.showSuggestion(hold.text)
}

// warnSuggestion surfaces a broken suggester once per REPL process. The feature
// runs after every completed turn, so a repeated warning row would bury the
// conversation; the debug log still records every lifecycle result.
func (m *Model) warnSuggestion(text string) {
	if m.suggestion.warned {
		return
	}
	m.suggestion.warned = true
	m.appendItem(ui.Item{Kind: ui.KindWarning, Text: text})
}

// clearSuggestion removes the visible ghost, held result, and filled-text
// provenance, and cancels any in-flight request. It is called for actions that
// supersede a suggestion: history recall, completion, submit, a new session, a
// resume, and quit. Ordinary typing uses hideSuggestion so the ghost can return
// when the composer is empty again.
func (m *Model) clearSuggestion() {
	m.suggestion.dropHeld()
	m.suggestion.filledText = ""
	m.suggestion.hydrated = false
	m.cancelSuggestion()
	m.writeSuggestionGhost("")
}

// hideSuggestion keeps a shown ghost available for restore but cancels any
// in-flight request, so a late result cannot overwrite the user's draft. The
// placeholder is deliberately left in place: it is not rendered while the
// composer has text, and restoring it after the text is cleared is then free.
func (m *Model) hideSuggestion() {
	m.cancelSuggestion()
}

// fillSuggestion copies a shown, already-normalized suggestion into the
// composer as editable text without starting a run. The ghost is cleared so the
// text is now a draft; filledText preserves the model-output provenance until
// the user edits it or clears it.
func (m *Model) fillSuggestion() {
	text := m.suggestion.ghost
	if text == "" {
		return
	}
	m.clearSuggestion()
	m.input.SetValue(text)
	m.suggestion.filledText = text
	m.layout()
	m.contentChanged = true
}

// filledSuggestion reports whether the composer still holds exactly the text
// copied from a suggestion. Slash-command parsing is bypassed only in this
// state; any edit makes the text the user's own.
func (m Model) filledSuggestion() bool {
	return m.suggestion.filledText != "" && strings.TrimSpace(m.input.Value()) == m.suggestion.filledText
}

// cancelSuggestion stops an in-flight request and invalidates its identity. The
// child may take a moment to exit, but its late result is ignored. The drain
// channel stays set until that result arrives so an exit cannot race the child;
// canSuggest refuses to start another request until it is reaped.
func (m *Model) cancelSuggestion() {
	m.suggestion.invalidate()
}

// suggestionActive reports whether a ghost suggestion is currently visible. A
// ghost hidden behind a draft keeps its state but is not active: the footer and
// submit path must behave like an ordinary draft until the composer is empty.
func (m Model) suggestionActive() bool {
	return m.suggestionPhase() == suggestionShown && m.input.Value() == ""
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
