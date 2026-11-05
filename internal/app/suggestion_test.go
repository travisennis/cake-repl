package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/travisennis/cake-repl/internal/cake"
	"github.com/travisennis/cake-repl/internal/childproc"
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
	markCompletedTurn(&m)
	return m
}

// suggestionRunReady is the same completed turn with the run that produced it
// still active, for tests that drive finishRun directly.
func suggestionRunReady(t *testing.T) Model {
	t.Helper()
	m := suggestionReady(t)
	beginTestRun(&m)
	return m
}

// startSuggestionForTest starts one request and fails unless the model entered
// the suggesting state. The returned command is never executed; tests fabricate
// the result so no subprocess or model call runs.
func startSuggestionForTest(t *testing.T, m Model) Model {
	t.Helper()
	next, cmd := m.maybeStartSuggestion()
	if cmd == nil || next.suggestionPhase() != suggestionInFlight {
		t.Fatalf("maybeStartSuggestion = phase %v, cmd nil %v; want an in-flight request", next.suggestionPhase(), cmd == nil)
	}
	return next
}

// finishSuggestionForTest fabricates one result for the current request.
func finishSuggestionForTest(t *testing.T, m Model, res suggest.Result) Model {
	t.Helper()
	key := m.suggestion.request.key
	if key.request == 0 {
		t.Fatal("no suggestion request is in flight")
	}
	return m.finishSuggestion(suggestionDoneMsg{key: key, res: res})
}

func suggestionBool(v bool) *bool { return &v }

func suggestionDocument(text string) *suggest.Document {
	return &suggest.Document{
		Suggested:  suggestionBool(true),
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
		{"no completion record", func(m *Model) {
			m.session.LastComplete = nil
			m.runOutcome = outcomeFailure
		}, false},
		{"errored completion", func(m *Model) {
			m.session.LastComplete.IsError = true
			m.runOutcome = outcomeFailure
		}, false},
		{"no pinned session", func(m *Model) { m.session.Reset() }, false},
		{"draft in the composer", func(m *Model) { m.input.SetValue("draft") }, false},
		{"request already in flight", func(m *Model) {
			m.suggestion.begin(m.session.Generation(), suggestionSessionID, nil, make(chan struct{}))
		}, false},
		{"ghost already showing", func(m *Model) { m.showSuggestion("shown") }, false},
		{"held result waiting for compaction", func(m *Model) {
			m.suggestion.hold("held", operationKey{generation: m.session.Generation(), request: 1, sessionID: suggestionSessionID})
		}, false},
		{"run in flight", func(m *Model) { beginTestRun(m) }, false},
		{"run draining", func(m *Model) { beginTestRun(m); m.runOp.cancel() }, false},
		{"compaction in flight", func(m *Model) { m.compaction.begin(operationKey{request: 1}, nil, make(chan struct{})) }, true},
		{"hydrating", func(m *Model) { m.hydrating = true }, false},
		{"replay pending", func(m *Model) { m.replayPending = true }, false},
		{"session browser open", func(m *Model) { m.browserOpen = true }, false},
		{"exit pending", func(m *Model) { m.exitPhase = phaseDraining }, false},
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

// TestAdmitSuggestion covers the suggestion-specific admission check. It must
// ignore compaction, because ADR 031 starts both operations together, and its
// refusal is never user-facing.
func TestAdmitSuggestion(t *testing.T) {
	tests := []struct {
		name  string
		setup func(*Model)
		want  bool
	}{
		{"idle", func(*Model) {}, true},
		{"exiting", func(m *Model) { m.exitPhase = phaseDraining }, false},
		{"browser open", func(m *Model) { m.browserOpen = true }, false},
		{"run in flight", func(m *Model) { beginTestRun(m) }, false},
		{"run draining", func(m *Model) { beginTestRun(m); m.runOp.cancel() }, false},
		{"replay loading", func(m *Model) { m.hydrating = true }, false},
		{"replay pending", func(m *Model) { m.replayPending = true }, false},
		{"compaction runs alongside", func(m *Model) {
			m.compaction.begin(operationKey{request: 1}, nil, make(chan struct{}))
		}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := suggestionReady(t)
			tt.setup(&m)
			gate := m.admitSuggestion()
			if gate.allowed != tt.want {
				t.Fatalf("admitSuggestion = %+v, want allowed=%v", gate, tt.want)
			}
			if gate.text != "" {
				t.Fatalf("suggestion refusal text = %q, want silent", gate.text)
			}
		})
	}
}

// TestSuggestionPhaseTransitions covers the single phase used by disabled,
// idle, in-flight, draining, held, and shown suggestions, and the one-writer
// invariant tying the ghost text to the composer placeholder.
func TestSuggestionPhaseTransitions(t *testing.T) {
	m := suggestionReady(t)
	if got := m.suggestionPhase(); got != suggestionIdle {
		t.Fatalf("ready phase = %v, want idle", got)
	}

	m.cfg.Suggestions.Enabled = false
	if got := m.suggestionPhase(); got != suggestionDisabled {
		t.Fatalf("disabled phase = %v, want disabled", got)
	}
	m.cfg.Suggestions.Enabled = true

	m = startSuggestionForTest(t, m)
	if got := m.suggestionPhase(); got != suggestionInFlight {
		t.Fatalf("started phase = %v, want in flight", got)
	}
	key := m.suggestion.request.key

	m.clearSuggestion()
	if got := m.suggestionPhase(); got != suggestionDraining {
		t.Fatalf("cleared phase = %v, want draining", got)
	}
	m = m.finishSuggestion(suggestionDoneMsg{key: key, res: suggest.Result{Canceled: true}})
	if got := m.suggestionPhase(); got != suggestionIdle {
		t.Fatalf("reaped phase = %v, want idle", got)
	}

	m = startSuggestionForTest(t, m)
	m = finishSuggestionForTest(t, m, suggest.Result{
		ExitCode: 0,
		Document: suggestionDocument("Try the lifecycle test."),
	})
	if got := m.suggestionPhase(); got != suggestionShown {
		t.Fatalf("applied phase = %v, want shown", got)
	}
	if m.suggestion.ghost != m.input.Placeholder || m.suggestion.ghost != "Try the lifecycle test." {
		t.Fatalf("ghost and placeholder disagree: ghost=%q placeholder=%q", m.suggestion.ghost, m.input.Placeholder)
	}

	m.clearSuggestion()
	if got := m.suggestionPhase(); got != suggestionIdle {
		t.Fatalf("dismissed phase = %v, want idle", got)
	}
	if m.suggestion.ghost != "" || m.input.Placeholder != defaultInputPlaceholder {
		t.Fatalf("dismissed presentation = ghost %q placeholder %q", m.suggestion.ghost, m.input.Placeholder)
	}

	m.suggestion.hold("Held until compaction settles.", operationKey{
		generation: m.session.Generation(),
		request:    99,
		sessionID:  suggestionSessionID,
	})
	if got := m.suggestionPhase(); got != suggestionHeld {
		t.Fatalf("held phase = %v, want held", got)
	}
	m.suggestion.dropHeld()
	if got := m.suggestionPhase(); got != suggestionIdle {
		t.Fatalf("dropped held phase = %v, want idle", got)
	}
}

// TestSuggestionPhaseDoesNotGateAdmission proves the suggestion never joins
// the prompt or retarget resource gate, whether its child runs, drains, or the
// result is held or shown.
func TestSuggestionPhaseDoesNotGateAdmission(t *testing.T) {
	tests := []struct {
		name  string
		setup func(*Model)
	}{
		{"in flight", func(m *Model) {
			m.suggestion.begin(m.session.Generation(), suggestionSessionID, nil, make(chan struct{}))
		}},
		{"draining", func(m *Model) {
			m.suggestion.begin(m.session.Generation(), suggestionSessionID, nil, make(chan struct{}))
			m.suggestion.invalidate()
		}},
		{"held", func(m *Model) {
			m.suggestion.hold("held", operationKey{
				generation: m.session.Generation(),
				request:    1,
				sessionID:  suggestionSessionID,
			})
		}},
		{"shown", func(m *Model) { m.showSuggestion("shown") }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := suggestionReady(t)
			tt.setup(&m)
			if gate := m.admitPrompt(); !gate.allowed {
				t.Fatalf("prompt admission = %+v, want allowed", gate)
			}
			if gate := m.admitRetarget(); !gate.allowed {
				t.Fatalf("retarget admission = %+v, want allowed", gate)
			}
		})
	}
}

func TestFinishSuggestionAppliesDocument(t *testing.T) {
	m := startSuggestionForTest(t, suggestionReady(t))
	before := len(m.timeline.items)
	m = finishSuggestionForTest(t, m, suggest.Result{
		ExitCode: 0,
		Document: suggestionDocument("Add a test for the new path."),
	})

	if m.suggestion.ghost != "Add a test for the new path." {
		t.Fatalf("suggestion ghost = %q", m.suggestion.ghost)
	}
	if m.suggestionPhase() != suggestionShown {
		t.Fatalf("suggestion phase = %v, want shown", m.suggestionPhase())
	}
	if m.input.Placeholder != "Add a test for the new path." {
		t.Fatalf("placeholder = %q", m.input.Placeholder)
	}
	if m.input.Value() != "" {
		t.Fatalf("input value = %q, want the ghost to stay out of the value", m.input.Value())
	}
	if m.suggestion.request.key.request != 0 || m.suggestion.doneChannel() != nil {
		t.Fatalf("request state not settled: req=%d done=%v", m.suggestion.request.key.request, m.suggestion.doneChannel() != nil)
	}
	if len(m.timeline.items) != before {
		t.Fatalf("suggestion added %d timeline items, want none", len(m.timeline.items)-before)
	}
	if got := m.composerHint(); got != "Ctrl+S use suggestion · type to dismiss" {
		t.Fatalf("composerHint = %q", got)
	}
}

func TestSuggestionFromFakeBinary(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "fake-cake-suggest")
	script := "#!/bin/sh\nprintf '%s\\n' '{\"suggested\":true,\"suggestion\":\"Add a test for the new path.\",\"session_id\":\"" +
		suggestionSessionID + "\"}'\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	ready := suggestionReady(t)
	ready.cfg.Suggestions.Bin = bin
	m, cmd := ready.maybeStartSuggestion()
	if cmd == nil {
		t.Fatal("maybeStartSuggestion returned no command")
	}

	msg := cmd()
	done, ok := msg.(suggestionDoneMsg)
	if !ok {
		t.Fatalf("command produced %T, want suggestionDoneMsg", msg)
	}
	updated, _ := m.Update(done)
	m = updated.(Model)

	if m.input.Placeholder != "Add a test for the new path." || !m.suggestionActive() {
		t.Fatalf("ghost = %q (active %v), want the fake suggester's document", m.input.Placeholder, m.suggestionActive())
	}
	if m.suggestion.doneChannel() != nil {
		t.Fatal("suggestion drain channel still set after the fake child was reaped")
	}
	if got := m.composerHint(); got != "Ctrl+S use suggestion \u00b7 type to dismiss" {
		t.Fatalf("composerHint = %q", got)
	}
}

// TestSuggestionSelection checks the resolved -model/-profile/-reasoning-effort
// passed to the suggester: the [suggestions] value wins, each unset model or
// profile falls back independently to the turn selection, and an empty effort
// omits the flag.
func TestSuggestionSelection(t *testing.T) {
	tests := []struct {
		name              string
		suggestionModel   string
		suggestionProfile string
		suggestionEffort  string
		turnModel         string
		turnProfile       string
		wantModel         string
		wantProfile       string
		wantEffort        string
	}{
		{
			name:              "suggestion selection overrides the turn",
			suggestionModel:   "suggestion-model",
			suggestionProfile: "suggestion-profile",
			turnModel:         "turn-model",
			turnProfile:       "turn-profile",
			wantModel:         "suggestion-model",
			wantProfile:       "suggestion-profile",
		},
		{
			name:        "unset suggestion values fall back",
			turnModel:   "turn-model",
			turnProfile: "turn-profile",
			wantModel:   "turn-model",
			wantProfile: "turn-profile",
		},
		{
			name:            "model and profile fall back independently",
			suggestionModel: "suggestion-model",
			turnModel:       "turn-model",
			turnProfile:     "turn-profile",
			wantModel:       "suggestion-model",
			wantProfile:     "turn-profile",
		},
		{
			name:             "configured reasoning effort is passed",
			suggestionEffort: "low",
			wantEffort:       "low",
		},
		{
			name: "empty reasoning effort is omitted",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			argsPath := filepath.Join(t.TempDir(), "args")
			t.Setenv("ARGS_FILE", argsPath)
			bin := filepath.Join(t.TempDir(), "fake-cake-suggest")
			script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$ARGS_FILE\"\n" +
				"printf '%s\\n' '{\"suggested\":false,\"session_id\":\"" + suggestionSessionID + "\",\"reason\":\"done\"}'\n"
			if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
				t.Fatal(err)
			}

			m := suggestionReady(t)
			m.cfg.Suggestions.Bin = bin
			m.cfg.Suggestions.Model = tt.suggestionModel
			m.cfg.Suggestions.Profile = tt.suggestionProfile
			m.cfg.Suggestions.ReasoningEffort = tt.suggestionEffort
			m.cfg.Model = tt.turnModel
			m.cfg.Profile = tt.turnProfile
			m, cmd := m.maybeStartSuggestion()
			if cmd == nil {
				t.Fatal("maybeStartSuggestion returned no command")
			}
			if _, ok := cmd().(suggestionDoneMsg); !ok {
				t.Fatal("suggester command did not produce a suggestionDoneMsg")
			}

			raw, err := os.ReadFile(argsPath)
			if err != nil {
				t.Fatal(err)
			}
			args := strings.Fields(string(raw))
			if got := argsValue(args, "-model"); got != tt.wantModel {
				t.Fatalf("-model = %q, want %q (args %v)", got, tt.wantModel, args)
			}
			if got := argsValue(args, "-profile"); got != tt.wantProfile {
				t.Fatalf("-profile = %q, want %q (args %v)", got, tt.wantProfile, args)
			}
			if got := argsValue(args, "-reasoning-effort"); got != tt.wantEffort {
				t.Fatalf("-reasoning-effort = %q, want %q (args %v)", got, tt.wantEffort, args)
			}
		})
	}
}

// argsValue returns the token following name in a captured argv, or an empty
// string when the flag is absent.
func argsValue(args []string, name string) string {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == name {
			return args[i+1]
		}
	}
	return ""
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
		t.Fatalf("ghost survived typing: text=%q placeholder=%q", m.suggestion.ghost, m.input.Placeholder)
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
	if cmd == nil || !m.runBusy() {
		t.Fatalf("submit did not start a run: running=%v cmd nil=%v", m.runBusy(), cmd == nil)
	}
	if got := m.timeline.items[len(m.timeline.items)-1]; got.Kind != ui.KindUser || got.Text != "/exit" {
		t.Fatalf("last item = %+v, want the suggestion as an ordinary user prompt", got)
	}
	if m.suggestionActive() || m.input.Placeholder != defaultInputPlaceholder {
		t.Fatalf("accepted suggestion survived submit: text=%q placeholder=%q", m.suggestion.ghost, m.input.Placeholder)
	}
	if m.input.Value() != "" {
		t.Fatalf("input value = %q, want reset", m.input.Value())
	}
	m.run.Cancel()
}

func TestSuggestionStaleResultIgnored(t *testing.T) {
	m := startSuggestionForTest(t, suggestionReady(t))
	key := m.suggestion.request.key
	m.clearSuggestion()

	next := m.finishSuggestion(suggestionDoneMsg{
		key: key,
		res: suggest.Result{
			ExitCode: 0,
			Document: suggestionDocument("stale"),
		},
	})
	if next.suggestionActive() || next.input.Placeholder != defaultInputPlaceholder {
		t.Fatalf("stale result changed the composer: text=%q placeholder=%q", next.suggestion.ghost, next.input.Placeholder)
	}
	if next.suggestion.doneChannel() != nil {
		t.Fatal("stale result left the drain channel set")
	}
}

func TestSuggestionWrongSessionIgnored(t *testing.T) {
	m := startSuggestionForTest(t, suggestionReady(t))
	doc := suggestionDocument("wrong session")
	doc.SessionID = "99999999-2222-3333-4444-555555555555"
	m = finishSuggestionForTest(t, m, suggest.Result{ExitCode: 0, Document: doc})
	if m.suggestionActive() {
		t.Fatalf("wrong-session result was applied: %q", m.suggestion.ghost)
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
		t.Fatalf("late result changed a draft: text=%q value=%q", m.suggestion.ghost, m.input.Value())
	}
}

func TestSuggestionFailureWarnsOnce(t *testing.T) {
	m := suggestionReady(t)
	for _, res := range []suggest.Result{
		{ExitCode: 1, Stderr: "cake-suggest: boom"},
		{ExitCode: -1, Err: errors.New("suggester timed out")},
	} {
		m = startSuggestionForTest(t, m)
		m = finishSuggestionForTest(t, m, res)
	}
	warnings := 0
	for _, it := range m.timeline.items {
		if it.Kind == ui.KindWarning && strings.Contains(it.Text, "prompt suggestion unavailable") {
			warnings++
		}
	}
	if warnings != 1 {
		t.Fatalf("warnings = %d, want exactly one per process", warnings)
	}
	if !m.suggestion.warned {
		t.Fatal("suggestion.warned = false")
	}
}

func TestSuggestionNoOpAndCancellationSilent(t *testing.T) {
	m := suggestionReady(t)
	for _, res := range []suggest.Result{
		{ExitCode: 0, Document: &suggest.Document{Suggested: suggestionBool(false), SessionID: suggestionSessionID}},
		{Canceled: true},
	} {
		m = startSuggestionForTest(t, m)
		before := len(m.timeline.items)
		m = finishSuggestionForTest(t, m, res)
		if len(m.timeline.items) != before {
			t.Fatalf("result %+v added %d timeline items, want none", res, len(m.timeline.items)-before)
		}
		if m.suggestionActive() || m.suggestion.warned {
			t.Fatalf("result %+v changed composer or warned", res)
		}
	}
}

func TestSuggestionCancelOnNewSession(t *testing.T) {
	m := startSuggestionForTest(t, suggestionReady(t))
	next, _ := m.startNewSession()
	m = next.(Model)
	if m.suggestionPhase() != suggestionDraining || m.suggestion.request.key.request != 0 {
		t.Fatalf("new session left the suggestion in phase %v, req=%d; want draining",
			m.suggestionPhase(), m.suggestion.request.key.request)
	}
	if m.suggestion.doneChannel() == nil {
		t.Fatal("new session dropped the drain channel before the child was reaped")
	}
}

func TestFinishRunStartsSuggestion(t *testing.T) {
	m := suggestionRunReady(t)
	next, cmd := m.finishRun(cake.Result{ExitCode: 0})
	finished := next.(Model)
	if cmd == nil || finished.suggestionPhase() != suggestionInFlight {
		t.Fatalf("finishRun did not start a suggestion: phase=%v cmd nil=%v", finished.suggestionPhase(), cmd == nil)
	}
	finished.cancelSuggestion()
}

func TestFinishRunFailureSkipsSuggestion(t *testing.T) {
	for _, res := range []cake.Result{
		{Canceled: true},
		{ExitCode: 1},
		{Err: os.ErrDeadlineExceeded},
	} {
		m := suggestionRunReady(t)
		next, cmd := m.finishRun(res)
		finished := next.(Model)
		if finished.suggestionBusy() || cmd == nil {
			// A nil cmd is fine only when the run did not start a suggestion;
			// finishRun always returns a title command, so require one.
			t.Fatalf("finishRun(%+v) started a suggestion: busy=%v cmd nil=%v", res, finished.suggestionBusy(), cmd == nil)
		}
	}
}

func TestFinishRunErroredCompletionSkipsSuggestion(t *testing.T) {
	m := suggestionRunReady(t)
	m.session.LastComplete.IsError = true
	next, cmd := m.finishRun(cake.Result{ExitCode: 0})
	finished := next.(Model)
	if finished.suggestionBusy() {
		t.Fatalf("errored completion started a suggestion: phase=%v", finished.suggestionPhase())
	}
	if cmd == nil {
		t.Fatal("finishRun returned no title command")
	}
}

func TestSuggestionMissingSuggestedWarns(t *testing.T) {
	m := startSuggestionForTest(t, suggestionReady(t))
	before := len(m.timeline.items)
	m = finishSuggestionForTest(t, m, suggest.Result{
		ExitCode: 0,
		Document: &suggest.Document{
			Suggestion: "orphan suggestion",
			SessionID:  suggestionSessionID,
		},
	})
	if m.suggestionActive() {
		t.Fatalf("malformed document was applied: %q", m.suggestion.ghost)
	}
	if !m.suggestion.warned {
		t.Fatal("malformed document did not set suggestion.warned")
	}
	if len(m.timeline.items) != before+1 {
		t.Fatalf("timeline items = %d, want one warning", len(m.timeline.items)-before)
	}
	if got := m.timeline.items[len(m.timeline.items)-1]; got.Kind != ui.KindWarning || got.Text != suggestionMalformedText {
		t.Fatalf("warning = %+v, want %q", got, suggestionMalformedText)
	}
}

func TestSuggestionDrainingChildBlocksNewRequest(t *testing.T) {
	m := startSuggestionForTest(t, suggestionReady(t))
	m.cancelSuggestion()
	if m.suggestionPhase() != suggestionDraining || m.suggestion.doneChannel() == nil {
		t.Fatalf("after cancel: phase=%v done=%v; want draining with a reap channel",
			m.suggestionPhase(), m.suggestion.doneChannel() != nil)
	}
	if m.canSuggest() {
		t.Fatal("canSuggest started a second request while the canceled child was draining")
	}
}

func TestSuggestionDrainDoesNotGatePrompt(t *testing.T) {
	m := startSuggestionForTest(t, suggestionReady(t))
	key := m.suggestion.request.key
	m.cfg.CakeBin = writeFakeCake(t, `
cat <<'EOF'
{"type":"task_complete","session_id":"`+suggestionSessionID+`","task_id":"t-1"}
EOF
`)
	m.input.SetValue("next prompt")

	tm, cmd := m.submit()
	m = tm.(Model)
	if cmd == nil || !m.runBusy() {
		t.Fatal("an in-flight suggestion blocked prompt submission")
	}
	if m.suggestionPhase() != suggestionDraining {
		t.Fatalf("submit left suggestion phase %v, want draining", m.suggestionPhase())
	}
	_ = cmd
	drainRun(t, m.run)

	updated, _ := m.Update(suggestionDoneMsg{
		key: key,
		res: suggest.Result{
			ExitCode: 0,
			Document: suggestionDocument("stale"),
		},
	})
	m = updated.(Model)
	if m.suggestionActive() || m.suggestionPhase() != suggestionIdle || m.suggestion.doneChannel() != nil {
		t.Fatalf("late suggestion result touched the composer: ghost=%q phase=%v done=%v",
			m.suggestion.ghost, m.suggestionPhase(), m.suggestion.doneChannel() != nil)
	}
}

func TestSuggestionDrainDoesNotGateNewSession(t *testing.T) {
	m := startSuggestionForTest(t, suggestionReady(t))
	key := m.suggestion.request.key

	tm, _ := m.execCommand(Command{Kind: CmdNew})
	m = tm.(Model)
	if mode, id := m.session.RunOptions(); mode != cake.RunFresh || id != "" {
		t.Fatalf("new session was gated by a suggestion drain: %v %q", mode, id)
	}
	if m.suggestionPhase() != suggestionDraining {
		t.Fatalf("/new left suggestion phase %v, want draining", m.suggestionPhase())
	}

	updated, _ := m.Update(suggestionDoneMsg{
		key: key,
		res: suggest.Result{Canceled: true},
	})
	m = updated.(Model)
	if m.suggestionActive() || m.suggestionPhase() != suggestionIdle || m.suggestion.doneChannel() != nil {
		t.Fatalf("late suggestion result crossed the new-session boundary: ghost=%q phase=%v done=%v",
			m.suggestion.ghost, m.suggestionPhase(), m.suggestion.doneChannel() != nil)
	}
}

func TestQuitDrainsSuggestion(t *testing.T) {
	m := startSuggestionForTest(t, suggestionReady(t))
	key := m.suggestion.request.key
	m, cmd := m.quit()
	if cmd != nil {
		t.Fatal("quit returned a command while the suggester child was in flight")
	}
	if !m.exiting() || m.suggestion.doneChannel() == nil {
		t.Fatalf("quit did not enter the drain state: exiting=%v done=%v", m.exiting(), m.suggestion.doneChannel() != nil)
	}
	if m.suggestionPhase() != suggestionDraining || m.suggestion.request.key.request != 0 || m.suggestion.request.cancelFn != nil {
		t.Fatalf("quit did not invalidate the request: phase=%v req=%d cancel=%v",
			m.suggestionPhase(), m.suggestion.request.key.request, m.suggestion.request.cancelFn != nil)
	}
	found := false
	for _, it := range m.timeline.items {
		if it.Kind == ui.KindWarning && strings.Contains(it.Text, "canceling prompt suggestion") {
			found = true
		}
	}
	if !found {
		t.Fatal("quit did not append the cancel-and-exit notice")
	}

	// While the canceled child is draining, neither a typed key nor a direct
	// submit may start new work behind the quit.
	m.input.SetValue("draft")
	next, cmd := m.submit()
	m = next.(Model)
	if cmd != nil || m.runBusy() {
		t.Fatalf("submit during quit drain started work: running=%v cmd nil=%v", m.runBusy(), cmd == nil)
	}
	next, keyCmd := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	m = next.(Model)
	if keyCmd != nil || m.input.Value() != "draft" {
		t.Fatalf("key during quit drain changed the composer: value=%q cmd nil=%v", m.input.Value(), keyCmd == nil)
	}

	updated, cmd := m.Update(suggestionDoneMsg{
		key: key,
		res: suggest.Result{Canceled: true},
	})
	m = updated.(Model)
	if cmd == nil {
		t.Fatal("the drained result did not return the quit command")
	}
	if m.suggestion.doneChannel() != nil || m.suggestionActive() || m.suggestionPhase() != suggestionIdle {
		t.Fatalf("drained result left state behind: phase=%v done=%v ghost=%q",
			m.suggestionPhase(), m.suggestion.doneChannel() != nil, m.suggestion.ghost)
	}
}

func TestQuitDrainsCanceledSuggestion(t *testing.T) {
	m := startSuggestionForTest(t, suggestionReady(t))
	m.clearSuggestion()
	if m.suggestion.doneChannel() == nil {
		t.Fatal("clearSuggestion dropped the drain channel before the child was reaped")
	}
	m, cmd := m.quit()
	if cmd != nil || !m.exiting() || m.suggestion.doneChannel() == nil {
		t.Fatalf("quit of a canceled child = cmd nil %v, exiting %v, done %v; want a drain",
			cmd == nil, m.exiting(), m.suggestion.doneChannel() != nil)
	}
}

func TestCancelRunningWaitsForSuggestionTeardown(t *testing.T) {
	m := suggestionReady(t)
	m.cfg.Children = childproc.NewRegistry()
	// Model an invalidated suggestion child whose reaping command the test
	// controls, without launching the real suggester.
	done := make(chan struct{})
	m.suggestion.begin(m.session.Generation(), suggestionSessionID, func() {}, done)
	m.trackChild("suggestion", func() {}, done)
	release := make(chan struct{})
	go func() {
		<-release
		close(done)
	}()
	returned := make(chan struct{})
	go func() {
		if err := m.CancelRunning(); err != nil {
			t.Errorf("CancelRunning: %v", err)
		}
		close(returned)
	}()
	select {
	case <-returned:
		t.Fatal("CancelRunning returned before the child was reaped")
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	select {
	case <-returned:
	case <-time.After(ChildTeardownTimeout):
		t.Fatal("CancelRunning did not return after the child was reaped")
	}
}

// TestHeldSuggestionAppliesAfterCanceledCompaction checks a held result follows
// the compaction phase from in flight through draining to idle, without ever
// joining the prompt gate itself.
func TestHeldSuggestionAppliesAfterCanceledCompaction(t *testing.T) {
	m := suggestionRunReady(t)
	m.cfg.Compaction = CompactionConfig{Enabled: true, Bin: "cake-compact", ThresholdPercent: 80}
	m.session.Context = &ContextUsage{Tokens: 170_000, Window: 200_000}

	next, _ := m.finishRun(cake.Result{ExitCode: 0})
	m = next.(Model)
	m = finishSuggestionForTest(t, m, suggest.Result{
		ExitCode: 0,
		Document: suggestionDocument("Add the canceled-compaction test."),
	})
	if got := m.suggestionPhase(); got != suggestionHeld {
		t.Fatalf("suggestion phase = %v, want held", got)
	}

	m.cancelCompaction()
	if !m.compactionBusy() || !m.compaction.draining() {
		t.Fatalf("compaction did not enter draining: busy=%v phase=%v", m.compactionBusy(), m.compaction.phase)
	}
	if gate := m.admitPrompt(); gate.allowed {
		t.Fatal("compaction drain opened the prompt gate")
	}

	m, _ = m.finishCompaction(compactionDoneMsg{
		req:       m.compaction.key.request,
		sessionID: suggestionSessionID,
		res:       compact.Result{Canceled: true},
	})
	if m.compactionBusy() {
		t.Fatal("canceled compaction did not settle")
	}
	if got := m.suggestionPhase(); got != suggestionShown {
		t.Fatalf("suggestion phase after compaction settle = %v, want shown", got)
	}
	if m.suggestion.ghost != "Add the canceled-compaction test." || m.input.Placeholder != m.suggestion.ghost {
		t.Fatalf("held result presentation = ghost %q placeholder %q", m.suggestion.ghost, m.input.Placeholder)
	}
}

// TestCompactionAndSuggestionRunTogether checks the end-of-turn transition
// starts both background calls and that a suggestion result arriving while the
// compacting gate is closed is held until the compactor settles.
func TestCompactionAndSuggestionRunTogether(t *testing.T) {
	m := suggestionRunReady(t)
	m.cfg.Compaction = CompactionConfig{Enabled: true, Bin: "cake-compact", ThresholdPercent: 80}
	m.session.Context = &ContextUsage{Tokens: 170_000, Window: 200_000}

	next, cmd := m.finishRun(cake.Result{ExitCode: 0})
	m = next.(Model)
	if cmd == nil || !m.compactionBusy() || m.suggestionPhase() != suggestionInFlight {
		t.Fatalf("finishRun did not start both: compacting=%v suggestion phase=%v cmd nil=%v", m.compactionBusy(), m.suggestionPhase(), cmd == nil)
	}

	m = finishSuggestionForTest(t, m, suggest.Result{
		ExitCode: 0,
		Document: suggestionDocument("Add a test for the parallel path."),
	})
	if m.suggestionPhase() != suggestionHeld || m.suggestion.ghost != "" || m.input.Placeholder != defaultInputPlaceholder {
		t.Fatalf("held result state: phase=%v ghost=%q placeholder=%q", m.suggestionPhase(), m.suggestion.ghost, m.input.Placeholder)
	}
	if m.suggestion.held.text != "Add a test for the parallel path." {
		t.Fatalf("held suggestion = %q", m.suggestion.held.text)
	}

	m, cmd = m.finishCompaction(compactionDoneMsg{
		req:       m.compaction.key.request,
		sessionID: suggestionSessionID,
		res:       compact.Result{ExitCode: 1, Stderr: "compactor failed"},
	})
	if m.compactionBusy() || m.suggestionPhase() != suggestionShown || cmd == nil {
		t.Fatalf("finishCompaction state: compacting=%v suggestion phase=%v cmd nil=%v", m.compactionBusy(), m.suggestionPhase(), cmd == nil)
	}
	if m.suggestion.held.text != "" {
		t.Fatalf("held suggestion survived the settle: %q", m.suggestion.held.text)
	}
	if m.suggestion.ghost != "Add a test for the parallel path." || m.input.Placeholder != "Add a test for the parallel path." {
		t.Fatalf("held suggestion was not applied: ghost=%q placeholder=%q", m.suggestion.ghost, m.input.Placeholder)
	}
}

// TestStaleCompactionResultDropsHeldSuggestion checks a result for a
// superseded compaction request cannot keep a held suggestion alive.
func TestStaleCompactionResultDropsHeldSuggestion(t *testing.T) {
	m := suggestionReady(t)
	m.suggestion.hold("Add a test for the parallel path.", operationKey{
		generation: m.session.Generation(),
		request:    1,
		sessionID:  suggestionSessionID,
	})
	m.compaction.begin(operationKey{request: 2}, nil, make(chan struct{}))

	m, cmd := m.finishCompaction(compactionDoneMsg{
		req:       1,
		sessionID: suggestionSessionID,
		res:       compact.Result{ExitCode: 1, Stderr: "superseded"},
	})
	if cmd != nil {
		t.Fatal("stale compaction result returned a command")
	}
	if !m.compactionBusy() {
		t.Fatal("stale compaction result settled the live gate")
	}
	if m.suggestion.held.text != "" || m.suggestion.held.key.sessionID != "" {
		t.Fatalf("held suggestion survived: text=%q session=%q", m.suggestion.held.text, m.suggestion.held.key.sessionID)
	}
}

// TestCompactionSettleDropsHeldSuggestionWhenComposerChanges covers the held
// result losing its claim before the compactor opens the gate.
func TestCompactionSettleDropsHeldSuggestionWhenComposerChanges(t *testing.T) {
	m := suggestionRunReady(t)
	m.cfg.Compaction = CompactionConfig{Enabled: true, Bin: "cake-compact", ThresholdPercent: 80}
	m.session.Context = &ContextUsage{Tokens: 170_000, Window: 200_000}

	next, _ := m.finishRun(cake.Result{ExitCode: 0})
	m = next.(Model)
	m = finishSuggestionForTest(t, m, suggest.Result{
		ExitCode: 0,
		Document: suggestionDocument("Add a test for the parallel path."),
	})
	m.input.SetValue("my own draft")
	m.clearSuggestion()

	m, _ = m.finishCompaction(compactionDoneMsg{
		req:       m.compaction.key.request,
		sessionID: suggestionSessionID,
		res:       compact.Result{ExitCode: 1, Stderr: "compactor failed"},
	})
	if m.suggestionActive() || m.suggestion.held.text != "" {
		t.Fatalf("dismissed held result survived settling: ghost=%q held=%q", m.suggestion.ghost, m.suggestion.held.text)
	}
}

// TestNewSessionDropsHeldSuggestion checks the new-session boundary discards a
// result held for the old session before the compactor settles.
func TestNewSessionDropsHeldSuggestion(t *testing.T) {
	m := suggestionRunReady(t)
	m.cfg.Compaction = CompactionConfig{Enabled: true, Bin: "cake-compact", ThresholdPercent: 80}
	m.session.Context = &ContextUsage{Tokens: 170_000, Window: 200_000}

	next, _ := m.finishRun(cake.Result{ExitCode: 0})
	m = next.(Model)
	m = finishSuggestionForTest(t, m, suggest.Result{
		ExitCode: 0,
		Document: suggestionDocument("Add a test for the parallel path."),
	})
	if m.suggestionPhase() != suggestionHeld {
		t.Fatalf("suggestion phase = %v, want held during compaction", m.suggestionPhase())
	}

	next, _ = m.startNewSession()
	m = next.(Model)
	m, _ = m.finishCompaction(compactionDoneMsg{
		req:       m.compaction.key.request,
		sessionID: suggestionSessionID,
		res:       compact.Result{ExitCode: 1, Stderr: "compactor failed"},
	})
	if m.suggestionActive() || m.suggestion.held.text != "" {
		t.Fatalf("held result crossed the new-session boundary: ghost=%q held=%q", m.suggestion.ghost, m.suggestion.held.text)
	}
}

// TestFinishCompactionDoesNotStartSuggestion checks that settling a compaction
// does not spend a second suggestion call after the turn's one attempt.
func TestFinishCompactionDoesNotStartSuggestion(t *testing.T) {
	m := suggestionRunReady(t)
	m.cfg.Compaction = CompactionConfig{Enabled: true, Bin: "cake-compact", ThresholdPercent: 80}
	m.session.Context = &ContextUsage{Tokens: 170_000, Window: 200_000}

	next, _ := m.finishRun(cake.Result{ExitCode: 0})
	m = next.(Model)
	m = finishSuggestionForTest(t, m, suggest.Result{
		ExitCode: 0,
		Document: &suggest.Document{
			Suggested: suggestionBool(false),
			SessionID: suggestionSessionID,
			Reason:    "the work is complete",
		},
	})

	m, _ = m.finishCompaction(compactionDoneMsg{
		req:       m.compaction.key.request,
		sessionID: suggestionSessionID,
		res:       compact.Result{ExitCode: 1, Stderr: "compactor failed"},
	})
	if m.suggestionPhase() == suggestionInFlight || m.suggestion.request.key.request != 0 {
		t.Fatalf("finishCompaction started a second request: phase=%v req=%d", m.suggestionPhase(), m.suggestion.request.key.request)
	}
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
