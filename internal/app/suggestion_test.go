package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/travisennis/cake-repl/internal/cake"
	"github.com/travisennis/cake-repl/internal/compact"
	"github.com/travisennis/cake-repl/internal/suggest"
	"github.com/travisennis/cake-repl/internal/ui"
)

const suggestionSessionID = "11111111-2222-3333-4444-555555555555"

// suggestionReady returns an idle, pinned model with one completed turn and an
// empty composer, which is the state in which a suggestion may start.
func suggestionReady(t *testing.T) Model {
	t.Helper()
	m := laidOut(160, 24)
	m.cfg.Suggestions = SuggestionConfig{Enabled: true, Bin: "cake-suggest"}
	m.session.OnTaskStart(cake.TaskStart{SessionID: suggestionSessionID, TaskID: "t-1"})
	m.session.OnTaskComplete(success(suggestionSessionID))
	m.sawComplete = true
	return m
}

// startSuggestionForTest starts one request and fails unless the model entered
// the suggesting state. The returned command is never executed; tests fabricate
// the result so no subprocess or model call runs.
func startSuggestionForTest(t *testing.T, m Model) Model {
	t.Helper()
	next, cmd := m.maybeStartSuggestion()
	if cmd == nil || !next.suggesting {
		t.Fatalf("maybeStartSuggestion = suggesting %v, cmd nil %v; want a request", next.suggesting, cmd == nil)
	}
	return next
}

// finishSuggestionForTest fabricates one result for the current request.
func finishSuggestionForTest(t *testing.T, m Model, res suggest.Result) Model {
	t.Helper()
	req := m.suggestionReq
	if req == 0 {
		t.Fatal("no suggestion request is in flight")
	}
	return m.finishSuggestion(suggestionDoneMsg{
		req:       req,
		sessionID: suggestionSessionID,
		res:       res,
	})
}

func suggestionDocument(text string) *suggest.Document {
	return &suggest.Document{
		Suggested:  true,
		Suggestion: text,
		SessionID:  suggestionSessionID,
	}
}

func TestCanSuggest(t *testing.T) {
	tests := []struct {
		name  string
		setup func(*Model)
		want  bool
	}{
		{"idle completed pinned turn", func(*Model) {}, true},
		{"disabled", func(m *Model) { m.cfg.Suggestions.Enabled = false }, false},
		{"no session", func(m *Model) { m.cfg.NoSession = true }, false},
		{"no completion record", func(m *Model) { m.session.LastComplete = nil }, false},
		{"no pinned session", func(m *Model) { m.session.Reset() }, false},
		{"draft in the composer", func(m *Model) { m.input.SetValue("draft") }, false},
		{"request already in flight", func(m *Model) { m.suggesting = true }, false},
		{"ghost already showing", func(m *Model) { m.suggestionText = "shown" }, false},
		{"run in flight", func(m *Model) { m.running = true }, false},
		{"compaction in flight", func(m *Model) { m.compacting = true }, false},
		{"hydrating", func(m *Model) { m.hydrating = true }, false},
		{"replay pending", func(m *Model) { m.replayPending = true }, false},
		{"session browser open", func(m *Model) { m.browserOpen = true }, false},
		{"exit pending", func(m *Model) { m.exitAfter = true }, false},
		{"new session pending", func(m *Model) { m.newSessionPending = true }, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := suggestionReady(t)
			tt.setup(&m)
			if got := m.canSuggest(); got != tt.want {
				t.Fatalf("canSuggest = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFinishSuggestionAppliesDocument(t *testing.T) {
	m := startSuggestionForTest(t, suggestionReady(t))
	before := len(m.items)
	m = finishSuggestionForTest(t, m, suggest.Result{
		ExitCode: 0,
		Document: suggestionDocument("Add a test for the new path."),
	})

	if m.suggestionText != "Add a test for the new path." {
		t.Fatalf("suggestionText = %q", m.suggestionText)
	}
	if m.input.Placeholder != "Add a test for the new path." {
		t.Fatalf("placeholder = %q", m.input.Placeholder)
	}
	if m.input.Value() != "" {
		t.Fatalf("input value = %q, want the ghost to stay out of the value", m.input.Value())
	}
	if m.suggesting || m.suggestionReq != 0 || m.suggestionCancel != nil {
		t.Fatalf("request state not settled: suggesting=%v req=%d cancel=%v", m.suggesting, m.suggestionReq, m.suggestionCancel != nil)
	}
	if len(m.items) != before {
		t.Fatalf("suggestion added %d timeline items, want none", len(m.items)-before)
	}
	if got := m.composerHint(); got != "Ctrl+S use suggestion · type to dismiss" {
		t.Fatalf("composerHint = %q", got)
	}
}

func TestSuggestionDismissedByTyping(t *testing.T) {
	m := startSuggestionForTest(t, suggestionReady(t))
	m = finishSuggestionForTest(t, m, suggest.Result{
		ExitCode: 0,
		Document: suggestionDocument("Add a test for the new path."),
	})

	next, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	m = next.(Model)
	if got := m.input.Value(); got != "x" {
		t.Fatalf("input value = %q, want only the typed character", got)
	}
	if m.suggestionActive() || m.input.Placeholder != defaultInputPlaceholder {
		t.Fatalf("ghost survived typing: text=%q placeholder=%q", m.suggestionText, m.input.Placeholder)
	}
	if got := m.composerHint(); got != "Ctrl+S submit · Enter newline · /help" {
		t.Fatalf("composerHint = %q", got)
	}
}

func TestSubmitUsesSuggestionAsPrompt(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "fake-cake")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	m := startSuggestionForTest(t, suggestionReady(t))
	m.cfg.CakeBin = bin
	m = finishSuggestionForTest(t, m, suggest.Result{
		ExitCode: 0,
		Document: suggestionDocument("/exit"),
	})

	next, cmd := m.submit()
	m = next.(Model)
	if cmd == nil || !m.running {
		t.Fatalf("submit did not start a run: running=%v cmd nil=%v", m.running, cmd == nil)
	}
	if got := m.items[len(m.items)-1]; got.Kind != ui.KindUser || got.Text != "/exit" {
		t.Fatalf("last item = %+v, want the suggestion as an ordinary user prompt", got)
	}
	if m.suggestionActive() || m.input.Placeholder != defaultInputPlaceholder {
		t.Fatalf("accepted suggestion survived submit: text=%q placeholder=%q", m.suggestionText, m.input.Placeholder)
	}
	if m.input.Value() != "" {
		t.Fatalf("input value = %q, want reset", m.input.Value())
	}
	m.run.Cancel()
}

func TestSuggestionStaleResultIgnored(t *testing.T) {
	m := startSuggestionForTest(t, suggestionReady(t))
	req := m.suggestionReq
	m.clearSuggestion()

	next := m.finishSuggestion(suggestionDoneMsg{
		req:       req,
		sessionID: suggestionSessionID,
		res: suggest.Result{
			ExitCode: 0,
			Document: suggestionDocument("stale"),
		},
	})
	if next.suggestionActive() || next.input.Placeholder != defaultInputPlaceholder {
		t.Fatalf("stale result changed the composer: text=%q placeholder=%q", next.suggestionText, next.input.Placeholder)
	}
}

func TestSuggestionWrongSessionIgnored(t *testing.T) {
	m := startSuggestionForTest(t, suggestionReady(t))
	doc := suggestionDocument("wrong session")
	doc.SessionID = "99999999-2222-3333-4444-555555555555"
	m = finishSuggestionForTest(t, m, suggest.Result{ExitCode: 0, Document: doc})
	if m.suggestionActive() {
		t.Fatalf("wrong-session result was applied: %q", m.suggestionText)
	}
}

func TestSuggestionDraftWinsOverLateResult(t *testing.T) {
	m := startSuggestionForTest(t, suggestionReady(t))
	m.input.SetValue("my own draft")
	m = finishSuggestionForTest(t, m, suggest.Result{
		ExitCode: 0,
		Document: suggestionDocument("late suggestion"),
	})
	if m.suggestionActive() || m.input.Value() != "my own draft" {
		t.Fatalf("late result changed a draft: text=%q value=%q", m.suggestionText, m.input.Value())
	}
}

func TestSuggestionFailureWarnsOnce(t *testing.T) {
	m := suggestionReady(t)
	for i := 0; i < 2; i++ {
		m = startSuggestionForTest(t, m)
		m = finishSuggestionForTest(t, m, suggest.Result{
			ExitCode: 1,
			Stderr:   "cake-suggest: boom",
		})
	}
	warnings := 0
	for _, it := range m.items {
		if it.Kind == ui.KindWarning && strings.Contains(it.Text, "prompt suggestion unavailable") {
			warnings++
		}
	}
	if warnings != 1 {
		t.Fatalf("warnings = %d, want exactly one per process", warnings)
	}
	if !m.suggestionWarned {
		t.Fatal("suggestionWarned = false")
	}
}

func TestSuggestionNoOpAndCancellationSilent(t *testing.T) {
	m := suggestionReady(t)
	for _, res := range []suggest.Result{
		{ExitCode: 0, Document: &suggest.Document{Suggested: false, SessionID: suggestionSessionID}},
		{Canceled: true},
	} {
		m = startSuggestionForTest(t, m)
		before := len(m.items)
		m = finishSuggestionForTest(t, m, res)
		if len(m.items) != before {
			t.Fatalf("result %+v added %d timeline items, want none", res, len(m.items)-before)
		}
		if m.suggestionActive() || m.suggestionWarned {
			t.Fatalf("result %+v changed composer or warned", res)
		}
	}
}

func TestSuggestionCancelOnNewSession(t *testing.T) {
	m := startSuggestionForTest(t, suggestionReady(t))
	next, _ := m.startNewSession()
	m = next.(Model)
	if m.suggesting || m.suggestionReq != 0 || m.suggestionCancel != nil {
		t.Fatalf("new session left a request in flight: suggesting=%v req=%d cancel=%v",
			m.suggesting, m.suggestionReq, m.suggestionCancel != nil)
	}
}

func TestFinishRunStartsSuggestion(t *testing.T) {
	m := suggestionReady(t)
	next, cmd := m.finishRun(cake.Result{ExitCode: 0})
	finished := next.(Model)
	if cmd == nil || !finished.suggesting {
		t.Fatalf("finishRun did not start a suggestion: suggesting=%v cmd nil=%v", finished.suggesting, cmd == nil)
	}
	finished.cancelSuggestion()
}

func TestFinishRunFailureSkipsSuggestion(t *testing.T) {
	for _, res := range []cake.Result{
		{Canceled: true},
		{ExitCode: 1},
		{Err: os.ErrDeadlineExceeded},
	} {
		m := suggestionReady(t)
		next, cmd := m.finishRun(res)
		finished := next.(Model)
		if finished.suggesting || cmd == nil {
			// A nil cmd is fine only when the run did not start a suggestion;
			// finishRun always returns a title command, so require one.
			t.Fatalf("finishRun(%+v) started a suggestion: suggesting=%v cmd nil=%v", res, finished.suggesting, cmd == nil)
		}
	}
}

func TestCompactionThenSuggestion(t *testing.T) {
	m := suggestionReady(t)
	m.cfg.Compaction = CompactionConfig{Enabled: true, Bin: "cake-compact", ThresholdPercent: 80}
	m.session.Context = &ContextUsage{Tokens: 170_000, Window: 200_000}

	next, cmd := m.finishRun(cake.Result{ExitCode: 0})
	m = next.(Model)
	if cmd == nil || !m.compacting || m.suggesting {
		t.Fatalf("finishRun did not start compaction first: compacting=%v suggesting=%v cmd nil=%v", m.compacting, m.suggesting, cmd == nil)
	}

	m, cmd = m.finishCompaction(compactionDoneMsg{
		req:       m.compactionReq,
		sessionID: suggestionSessionID,
		res:       compact.Result{ExitCode: 1, Stderr: "compactor failed"},
	})
	if m.compacting || !m.suggesting || cmd == nil {
		t.Fatalf("finishCompaction did not start the suggestion after settling: compacting=%v suggesting=%v cmd nil=%v", m.compacting, m.suggesting, cmd == nil)
	}
	m.cancelSuggestion()
}

func TestNormalizeSuggestion(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
		ok   bool
	}{
		{"plain", "  Add a test.  ", "Add a test.", true},
		{"multi-line and controls", "Add\x1b[31m a\n\ttest.", "Add a test.", true},
		{"empty", " \n\t ", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := normalizeSuggestion(tt.in)
			if got != tt.want || ok != tt.ok {
				t.Fatalf("normalizeSuggestion(%q) = %q, %v; want %q, %v", tt.in, got, ok, tt.want, tt.ok)
			}
		})
	}

	long := strings.Repeat("a", maxSuggestionBytes+10)
	got, ok := normalizeSuggestion(long)
	if !ok || len(got) > maxSuggestionBytes+len("…") {
		t.Fatalf("long suggestion = %d bytes, want bounded", len(got))
	}
}
