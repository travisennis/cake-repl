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
	suggestionTimeout  = 60 * time.Second
	maxSuggestionBytes = 4 * 1024
)

type suggestionDoneMsg struct {
	key operationKey
	res suggest.Result
}

type suggestionPhase uint8

const (
	suggestionDisabled suggestionPhase = iota
	suggestionIdle
	suggestionInFlight
	suggestionDraining
	suggestionHeld
	suggestionShown
)

type suggestionHold struct {
	text string
	key  operationKey
}

type suggestionState struct {
	request    pendingOperation
	seq        uint64
	ghost      string
	held       suggestionHold
	start      time.Time
	elapsed    time.Duration
	filledText string
	hydrated   bool
	warned     bool
}

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

func (s *suggestionState) begin(generation sessionGeneration, sessionID string, cancel context.CancelFunc, done <-chan struct{}) operationKey {
	s.seq++
	key := operationKey{generation: generation, request: s.seq, sessionID: sessionID}
	s.request.begin(key, cancel, done)
	s.start = time.Now()
	s.elapsed = 0
	return key
}

func (s *suggestionState) settle(key operationKey) bool {
	settled := s.request.settle(key)
	if !s.request.active() && !s.request.draining() {
		s.start = time.Time{}
		s.elapsed = 0
	}
	return settled
}

func (s *suggestionState) invalidate() {
	s.request.invalidate()
}

func (s *suggestionState) doneChannel() <-chan struct{} {
	return s.request.doneChannel()
}

func (s *suggestionState) hold(text string, key operationKey) {
	s.held = suggestionHold{text: text, key: key}
}

func (s *suggestionState) takeHeld() suggestionHold {
	hold := s.held
	s.held = suggestionHold{}
	return hold
}

func (s *suggestionState) dropHeld() {
	s.held = suggestionHold{}
}

func (m Model) suggestionPhase() suggestionPhase {
	return m.suggestion.phase(m.cfg.Suggestions.Enabled && !m.cfg.NoSession)
}

func (m Model) suggestionBusy() bool {
	p := m.suggestionPhase()
	return p == suggestionInFlight || p == suggestionDraining
}

func (m Model) suggestionInFlight() bool {
	return m.suggestionPhase() == suggestionInFlight
}

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
	if m.watchQueued() {
		return false
	}
	mode, sessionID := m.session.RunOptions()
	return mode == cake.RunResume && sessionID != ""
}

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
		Model:           firstNonEmpty(m.cfg.Suggestions.Model, m.effectiveModel()),
		Profile:         firstNonEmpty(m.cfg.Suggestions.Profile, m.effectiveProfile()),
		ReasoningEffort: m.cfg.Suggestions.ReasoningEffort,
		DebugLog:        m.cfg.DebugLog,
	}
	return m, func() tea.Msg {
		res := suggest.Suggest(ctx, opts)
		close(done)
		return suggestionDoneMsg{key: key, res: res}
	}
}

func (m Model) finishSuggestion(msg suggestionDoneMsg) Model {
	if !m.suggestion.settle(msg.key) {
		return m
	}
	if msg.key.generation != m.session.Generation() {
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
		m.warnSuggestion(suggestionMalformedText)
		m.syncViewport()
		return m
	}
	if !*doc.Suggested {
		m.logDebug("suggestion: no-op")
		return m
	}
	if doc.SessionID != msg.key.sessionID {
		m.logDebug("suggestion: session mismatch")
		return m
	}
	text, ok := normalizeSuggestion(doc.Suggestion)
	if !ok {
		return m
	}
	if !m.canSuggest() {
		return m
	}
	if _, pinned := m.session.RunOptions(); pinned != msg.key.sessionID {
		m.logDebug("suggestion: session pin changed")
		return m
	}
	if m.compactionBusy() {
		m.suggestion.hold(text, msg.key)
		return m
	}

	m.showSuggestion(text)
	return m
}

func (m *Model) showSuggestion(text string) {
	m.suggestion.dropHeld()
	m.writeSuggestionGhost(text)
	m.layout()
}

func (m *Model) writeSuggestionGhost(text string) {
	m.suggestion.ghost = text
	placeholder := defaultInputPlaceholder
	if text != "" {
		placeholder = text
	}
	if m.input.Placeholder != placeholder {
		m.input.Placeholder = placeholder
	}
}

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

func (m *Model) refreshSuggestionElapsed(now time.Time) {
	if !m.suggestionInFlight() || m.suggestion.start.IsZero() {
		return
	}
	elapsed := now.Sub(m.suggestion.start)
	if elapsed < 0 {
		elapsed = 0
	}
	m.suggestion.elapsed = elapsed.Truncate(time.Second)
}

func (m *Model) warnSuggestion(text string) {
	if m.suggestion.warned {
		return
	}
	m.suggestion.warned = true
	m.appendItem(ui.Item{Kind: ui.KindWarning, Text: text})
}

func (m *Model) clearSuggestion() {
	m.suggestion.dropHeld()
	m.suggestion.filledText = ""
	m.suggestion.hydrated = false
	m.cancelSuggestion()
	m.writeSuggestionGhost("")
}

func (m *Model) hideSuggestion() {
	m.cancelSuggestion()
}

func (m *Model) hideSuggestionForHistory() {
	m.suggestion.filledText = ""
	m.hideSuggestion()
}

func (m *Model) fillSuggestion() {
	text := m.suggestion.ghost
	if text == "" {
		return
	}
	m.clearSuggestion()
	if m.setComposerValue(text) {
		m.appendItem(ui.Item{Kind: ui.KindWarning, Text: composerTruncationWarning()})
	}
	m.suggestion.filledText = text
	m.layout()
}

func (m Model) filledSuggestion() bool {
	return m.suggestion.filledText != "" && strings.TrimSpace(m.input.Value()) == m.suggestion.filledText
}

func (m *Model) cancelSuggestion() {
	m.suggestion.invalidate()
}

func (m Model) suggestionActive() bool {
	return m.suggestionPhase() == suggestionShown && m.input.Value() == ""
}

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

const suggestionMalformedText = "prompt suggestion unavailable: malformed document (missing suggested)"

func suggestionFailureText(res suggest.Result, outputLimit int) string {
	text := "prompt suggestion unavailable"
	switch {
	case res.Err != nil:
		text += ": " + ui.TrimForDisplay(res.Err.Error(), outputLimit)
	case res.Stderr != "":
		text += ": " + ui.TrimForDisplay(res.Stderr, outputLimit)
	case res.ExitCode >= 0:
		text += fmt.Sprintf(" (exit code %d)", res.ExitCode)
	}
	return text
}
