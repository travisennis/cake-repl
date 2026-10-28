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
		{"no completion record", func(m *Model) { m.session.LastComplete = nil }, false},
		{"errored completion", func(m *Model) { m.session.LastComplete.IsError = true }, false},
		{"no pinned session", func(m *Model) { m.session.Reset() }, false},
		{"draft in the composer", func(m *Model) { m.input.SetValue("draft") }, false},
		{"request already in flight", func(m *Model) { m.suggesting = true }, false},
		{"ghost already showing", func(m *Model) { m.suggestionText = "shown" }, false},
		{"held result waiting for compaction", func(m *Model) { m.pendingSuggestion = "held" }, false},
		{"run in flight", func(m *Model) { m.running = true }, false},
		{"compaction in flight", func(m *Model) { m.compacting = true }, true},
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
	if m.suggesting || m.suggestionReq != 0 || m.suggestionCancel != nil || m.suggestionDone != nil {
		t.Fatalf("request state not settled: suggesting=%v req=%d cancel=%v done=%v", m.suggesting, m.suggestionReq, m.suggestionCancel != nil, m.suggestionDone != nil)
	}
	if len(m.items) != before {
		t.Fatalf("suggestion added %d timeline items, want none", len(m.items)-before)
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
	if m.suggestionDone != nil {
		t.Fatal("suggestionDone still set after the fake child was reaped")
	}
	if got := m.composerHint(); got != "Ctrl+S use suggestion \u00b7 type to dismiss" {
		t.Fatalf("composerHint = %q", got)
	}
}

// TestSuggestionModelSelection checks the resolved -model/-profile passed to
// the suggester: the [suggestions] value wins, and each unset value falls back
// independently to the turn selection.
func TestSuggestionModelSelection(t *testing.T) {
	tests := []struct {
		name              string
		suggestionModel   string
		suggestionProfile string
		turnModel         string
		turnProfile       string
		wantModel         string
		wantProfile       string
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
	if next.suggestionDone != nil {
		t.Fatal("stale result left the drain channel set")
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
	for _, res := range []suggest.Result{
		{ExitCode: 1, Stderr: "cake-suggest: boom"},
		{ExitCode: -1, Err: errors.New("suggester timed out")},
	} {
		m = startSuggestionForTest(t, m)
		m = finishSuggestionForTest(t, m, res)
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
		{ExitCode: 0, Document: &suggest.Document{Suggested: suggestionBool(false), SessionID: suggestionSessionID}},
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
	if m.suggestionDone == nil {
		t.Fatal("new session dropped the drain channel before the child was reaped")
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

func TestFinishRunErroredCompletionSkipsSuggestion(t *testing.T) {
	m := suggestionReady(t)
	m.session.LastComplete.IsError = true
	next, cmd := m.finishRun(cake.Result{ExitCode: 0})
	finished := next.(Model)
	if finished.suggesting || finished.suggestionDone != nil {
		t.Fatalf("errored completion started a suggestion: suggesting=%v done=%v",
			finished.suggesting, finished.suggestionDone != nil)
	}
	if cmd == nil {
		t.Fatal("finishRun returned no title command")
	}
}

func TestSuggestionMissingSuggestedWarns(t *testing.T) {
	m := startSuggestionForTest(t, suggestionReady(t))
	before := len(m.items)
	m = finishSuggestionForTest(t, m, suggest.Result{
		ExitCode: 0,
		Document: &suggest.Document{
			Suggestion: "orphan suggestion",
			SessionID:  suggestionSessionID,
		},
	})
	if m.suggestionActive() {
		t.Fatalf("malformed document was applied: %q", m.suggestionText)
	}
	if !m.suggestionWarned {
		t.Fatal("malformed document did not set suggestionWarned")
	}
	if len(m.items) != before+1 {
		t.Fatalf("timeline items = %d, want one warning", len(m.items)-before)
	}
	if got := m.items[len(m.items)-1]; got.Kind != ui.KindWarning || got.Text != suggestionMalformedText {
		t.Fatalf("warning = %+v, want %q", got, suggestionMalformedText)
	}
}

func TestSuggestionDrainingChildBlocksNewRequest(t *testing.T) {
	m := startSuggestionForTest(t, suggestionReady(t))
	m.cancelSuggestion()
	if m.suggestionDone == nil {
		t.Fatal("cancelSuggestion dropped the drain channel before the child was reaped")
	}
	if m.canSuggest() {
		t.Fatal("canSuggest started a second request while the canceled child was draining")
	}
}

func TestQuitDrainsSuggestion(t *testing.T) {
	m := startSuggestionForTest(t, suggestionReady(t))
	req := m.suggestionReq
	m, cmd := m.quit()
	if cmd != nil {
		t.Fatal("quit returned a command while the suggester child was in flight")
	}
	if !m.exitAfter || m.suggestionDone == nil {
		t.Fatalf("quit did not enter the drain state: exitAfter=%v done=%v", m.exitAfter, m.suggestionDone != nil)
	}
	if m.suggesting || m.suggestionReq != 0 || m.suggestionCancel != nil {
		t.Fatalf("quit did not invalidate the request: suggesting=%v req=%d cancel=%v",
			m.suggesting, m.suggestionReq, m.suggestionCancel != nil)
	}
	found := false
	for _, it := range m.items {
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
	if cmd != nil || m.running {
		t.Fatalf("submit during quit drain started work: running=%v cmd nil=%v", m.running, cmd == nil)
	}
	next, keyCmd := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	m = next.(Model)
	if keyCmd != nil || m.input.Value() != "draft" {
		t.Fatalf("key during quit drain changed the composer: value=%q cmd nil=%v", m.input.Value(), keyCmd == nil)
	}

	updated, cmd := m.Update(suggestionDoneMsg{
		req:       req,
		sessionID: suggestionSessionID,
		res:       suggest.Result{Canceled: true},
	})
	m = updated.(Model)
	if cmd == nil {
		t.Fatal("the drained result did not return the quit command")
	}
	if m.suggestionDone != nil || m.suggestionActive() {
		t.Fatalf("drained result left state behind: done=%v ghost=%q", m.suggestionDone != nil, m.suggestionText)
	}
}

func TestQuitDrainsCanceledSuggestion(t *testing.T) {
	m := startSuggestionForTest(t, suggestionReady(t))
	m.clearSuggestion()
	if m.suggestionDone == nil {
		t.Fatal("clearSuggestion dropped the drain channel before the child was reaped")
	}
	m, cmd := m.quit()
	if cmd != nil || !m.exitAfter || m.suggestionDone == nil {
		t.Fatalf("quit of a canceled child = cmd nil %v, exitAfter %v, done %v; want a drain",
			cmd == nil, m.exitAfter, m.suggestionDone != nil)
	}
}

func TestCancelRunningWaitsForSuggestionTeardown(t *testing.T) {
	m := startSuggestionForTest(t, suggestionReady(t))
	done := m.suggestionDone
	if done == nil {
		t.Fatal("suggestion has no drain channel")
	}
	release := make(chan struct{})
	go func() {
		<-release
		close(done)
	}()
	returned := make(chan struct{})
	go func() {
		m.CancelRunning()
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
	case <-time.After(suggestionTeardownTimeout):
		t.Fatal("CancelRunning did not return after the child was reaped")
	}
}

// TestCompactionAndSuggestionRunTogether checks the end-of-turn transition
// starts both background calls and that a suggestion result arriving while the
// compacting gate is closed is held until the compactor settles.
func TestCompactionAndSuggestionRunTogether(t *testing.T) {
	m := suggestionReady(t)
	m.cfg.Compaction = CompactionConfig{Enabled: true, Bin: "cake-compact", ThresholdPercent: 80}
	m.session.Context = &ContextUsage{Tokens: 170_000, Window: 200_000}

	next, cmd := m.finishRun(cake.Result{ExitCode: 0})
	m = next.(Model)
	if cmd == nil || !m.compacting || !m.suggesting {
		t.Fatalf("finishRun did not start both: compacting=%v suggesting=%v cmd nil=%v", m.compacting, m.suggesting, cmd == nil)
	}

	m = finishSuggestionForTest(t, m, suggest.Result{
		ExitCode: 0,
		Document: suggestionDocument("Add a test for the parallel path."),
	})
	if m.suggestionText != "" || m.input.Placeholder != defaultInputPlaceholder {
		t.Fatalf("ghost appeared while compaction gated the composer: text=%q placeholder=%q", m.suggestionText, m.input.Placeholder)
	}
	if m.pendingSuggestion != "Add a test for the parallel path." {
		t.Fatalf("pendingSuggestion = %q", m.pendingSuggestion)
	}

	m, cmd = m.finishCompaction(compactionDoneMsg{
		req:       m.compactionReq,
		sessionID: suggestionSessionID,
		res:       compact.Result{ExitCode: 1, Stderr: "compactor failed"},
	})
	if m.compacting || m.suggesting || cmd == nil {
		t.Fatalf("finishCompaction state: compacting=%v suggesting=%v cmd nil=%v", m.compacting, m.suggesting, cmd == nil)
	}
	if m.pendingSuggestion != "" {
		t.Fatalf("pendingSuggestion survived the settle: %q", m.pendingSuggestion)
	}
	if m.suggestionText != "Add a test for the parallel path." || m.input.Placeholder != "Add a test for the parallel path." {
		t.Fatalf("held suggestion was not applied: text=%q placeholder=%q", m.suggestionText, m.input.Placeholder)
	}
}

// TestStaleCompactionResultDropsHeldSuggestion checks a result for a
// superseded compaction request cannot keep a held suggestion alive.
func TestStaleCompactionResultDropsHeldSuggestion(t *testing.T) {
	m := suggestionReady(t)
	m.pendingSuggestion = "Add a test for the parallel path."
	m.pendingSuggestionSession = suggestionSessionID
	m.compactionReq = 2
	m.compacting = true

	m, cmd := m.finishCompaction(compactionDoneMsg{
		req:       1,
		sessionID: suggestionSessionID,
		res:       compact.Result{ExitCode: 1, Stderr: "superseded"},
	})
	if cmd != nil {
		t.Fatal("stale compaction result returned a command")
	}
	if !m.compacting {
		t.Fatal("stale compaction result settled the live gate")
	}
	if m.pendingSuggestion != "" || m.pendingSuggestionSession != "" {
		t.Fatalf("held suggestion survived: text=%q session=%q", m.pendingSuggestion, m.pendingSuggestionSession)
	}
}

// TestCompactionSettleDropsHeldSuggestionWhenComposerChanges covers the held
// result losing its claim before the compactor opens the gate.
func TestCompactionSettleDropsHeldSuggestionWhenComposerChanges(t *testing.T) {
	m := suggestionReady(t)
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
		req:       m.compactionReq,
		sessionID: suggestionSessionID,
		res:       compact.Result{ExitCode: 1, Stderr: "compactor failed"},
	})
	if m.suggestionActive() || m.pendingSuggestion != "" {
		t.Fatalf("dismissed held result survived settling: text=%q pending=%q", m.suggestionText, m.pendingSuggestion)
	}
}

// TestNewSessionDropsHeldSuggestion checks the new-session boundary discards a
// result held for the old session before the compactor settles.
func TestNewSessionDropsHeldSuggestion(t *testing.T) {
	m := suggestionReady(t)
	m.cfg.Compaction = CompactionConfig{Enabled: true, Bin: "cake-compact", ThresholdPercent: 80}
	m.session.Context = &ContextUsage{Tokens: 170_000, Window: 200_000}

	next, _ := m.finishRun(cake.Result{ExitCode: 0})
	m = next.(Model)
	m = finishSuggestionForTest(t, m, suggest.Result{
		ExitCode: 0,
		Document: suggestionDocument("Add a test for the parallel path."),
	})
	if m.pendingSuggestion == "" {
		t.Fatal("suggestion was not held during compaction")
	}

	next, _ = m.startNewSession()
	m = next.(Model)
	m, _ = m.finishCompaction(compactionDoneMsg{
		req:       m.compactionReq,
		sessionID: suggestionSessionID,
		res:       compact.Result{ExitCode: 1, Stderr: "compactor failed"},
	})
	if m.suggestionActive() || m.pendingSuggestion != "" {
		t.Fatalf("held result crossed the new-session boundary: text=%q pending=%q", m.suggestionText, m.pendingSuggestion)
	}
}

// TestFinishCompactionDoesNotStartSuggestion checks that settling a compaction
// does not spend a second suggestion call after the turn's one attempt.
func TestFinishCompactionDoesNotStartSuggestion(t *testing.T) {
	m := suggestionReady(t)
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
		req:       m.compactionReq,
		sessionID: suggestionSessionID,
		res:       compact.Result{ExitCode: 1, Stderr: "compactor failed"},
	})
	if m.suggesting || m.suggestionReq != 0 {
		t.Fatalf("finishCompaction started a second request: suggesting=%v req=%d", m.suggesting, m.suggestionReq)
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
