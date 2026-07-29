package app

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/travisennis/cake-repl/internal/cake"
	"github.com/travisennis/cake-repl/internal/ui"
)

// lastItem returns the most recently appended timeline item.
func lastItem(t *testing.T, m Model) ui.Item {
	t.Helper()
	if len(m.items) == 0 {
		t.Fatal("timeline is empty")
	}
	return m.items[len(m.items)-1]
}

// writeFakeCake writes an executable shell script standing in for the cake
// binary and returns its path.
func writeFakeCake(t *testing.T, script string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fake-cake")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// drainRun consumes a run's events and result so the subprocess is reaped
// before the test ends.
func drainRun(t *testing.T, run *cake.Run) {
	t.Helper()
	timeout := time.After(10 * time.Second)
	for {
		select {
		case _, ok := <-run.Events:
			if !ok {
				select {
				case <-run.Result:
					return
				case <-timeout:
					t.Fatal("timed out waiting for run result")
				}
			}
		case <-timeout:
			t.Fatal("timed out draining run events")
		}
	}
}

func TestDescribeHookDecisionVisibility(t *testing.T) {
	// Cake's full vocabulary (hooks.rs): none|deny|stop|error, plus allow
	// for resolved_decision. Only the benign two (and an absent field) are
	// hidden; everything else — including values cake does not emit today —
	// must be shown.
	for _, decision := range []string{"", "allow", "none"} {
		ev := cake.HookEvent{Event: "PreToolUse", Decision: decision}
		if line, show := describeHook(ev); show {
			t.Errorf("decision %q should stay hidden, got line %q", decision, line)
		}
	}
	for _, decision := range []string{"deny", "stop", "error", "ok", "success", "continue"} {
		ev := cake.HookEvent{Event: "PreToolUse", Decision: decision}
		if line, show := describeHook(ev); !show {
			t.Errorf("decision %q should be shown, got line %q", decision, line)
		}
	}
}

func TestDescribeHook(t *testing.T) {
	exit := func(code int) *int { return &code }
	tests := []struct {
		name     string
		event    cake.HookEvent
		wantShow bool
		wantLine string
	}{
		{
			name:     "no decision no exit stays hidden",
			event:    cake.HookEvent{Event: "PreToolUse", ToolName: "bash"},
			wantShow: false,
			wantLine: "hook PreToolUse [bash]",
		},
		{
			name:     "denial is shown with tool name",
			event:    cake.HookEvent{Event: "PreToolUse", ToolName: "bash", Decision: "deny"},
			wantShow: true,
			wantLine: "hook PreToolUse [bash] → deny",
		},
		{
			name:     "resolved decision overrides decision",
			event:    cake.HookEvent{Event: "PreToolUse", Decision: "deny", ResolvedDecision: "allow"},
			wantShow: false,
			wantLine: "hook PreToolUse → allow",
		},
		{
			name:     "resolved denial overrides benign decision",
			event:    cake.HookEvent{Event: "PreToolUse", Decision: "allow", ResolvedDecision: "stop"},
			wantShow: true,
			wantLine: "hook PreToolUse → stop",
		},
		{
			name:     "non-zero exit forces a benign decision visible",
			event:    cake.HookEvent{Event: "PostToolUse", Decision: "allow", ExitCode: exit(2)},
			wantShow: true,
			wantLine: "hook PostToolUse → allow (exit 2)",
		},
		{
			name:     "explicit zero exit stays hidden and unprinted",
			event:    cake.HookEvent{Event: "PostToolUse", Decision: "none", ExitCode: exit(0)},
			wantShow: false,
			wantLine: "hook PostToolUse → none",
		},
		{
			name:     "stderr keeps only its first line",
			event:    cake.HookEvent{Event: "PreToolUse", Decision: "deny", Stderr: "  bad command\nmore detail\n"},
			wantShow: true,
			wantLine: "hook PreToolUse → deny: bad command",
		},
		{
			name:     "whitespace-only stderr is ignored",
			event:    cake.HookEvent{Event: "PreToolUse", Decision: "deny", Stderr: " \n\t"},
			wantShow: true,
			wantLine: "hook PreToolUse → deny",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			line, show := describeHook(tt.event)
			if show != tt.wantShow {
				t.Errorf("show = %v, want %v", show, tt.wantShow)
			}
			if line != tt.wantLine {
				t.Errorf("line = %q, want %q", line, tt.wantLine)
			}
		})
	}
}

func TestToggleToolOutputCyclesGlobalMode(t *testing.T) {
	m := newLaidOutModel()
	m.applyEvent(cake.FunctionCall{ID: "fc-1", CallID: "c-1", Name: "bash", Arguments: `{"command":"make"}`})
	m.applyEvent(cake.FunctionCallOutput{CallID: "c-1", Output: strings.Repeat("x", ui.DefaultOutputLimit+100)})

	if m.toolOutputMode != ui.ToolOutputTruncated {
		t.Fatalf("default mode = %v, want truncated", m.toolOutputMode)
	}

	tm, _ := m.toggleToolOutput()
	m = tm.(Model)
	if m.toolOutputMode != ui.ToolOutputFull {
		t.Errorf("after first toggle: mode = %v, want full", m.toolOutputMode)
	}

	tm, _ = m.toggleToolOutput()
	m = tm.(Model)
	if m.toolOutputMode != ui.ToolOutputHidden {
		t.Errorf("after second toggle: mode = %v, want hidden", m.toolOutputMode)
	}

	tm, _ = m.toggleToolOutput()
	m = tm.(Model)
	if m.toolOutputMode != ui.ToolOutputTruncated {
		t.Errorf("after third toggle: mode = %v, want truncated", m.toolOutputMode)
	}
	assertCacheMatchesFullRender(t, &m, "after cycling output modes")
}

func TestToggleToolOutputRerendersEveryToolAndReusesNonToolCache(t *testing.T) {
	m := newLaidOutModel()
	firstOutput := strings.Repeat("x", ui.DefaultOutputLimit+1)
	secondOutput := strings.Repeat("z", ui.DefaultOutputLimit+1)
	m.applyEvent(cake.FunctionCall{ID: "fc-1", CallID: "c-1", Name: "bash", Arguments: `{"command":"ls"}`})
	m.applyEvent(cake.FunctionCallOutput{CallID: "c-1", Output: firstOutput})
	m.applyEvent(cake.FunctionCall{ID: "fc-2", CallID: "c-2", Name: "read", Arguments: `{"path":"main.go"}`})
	m.applyEvent(cake.FunctionCallOutput{CallID: "c-2", Output: secondOutput})

	// A sentinel proves the cached non-tool rendering is reused rather than
	// regenerated during the global tool-only pass.
	m.rendered[0] = "cached non-tool sentinel"

	tm, _ := m.toggleToolOutput()
	m = tm.(Model)
	if m.rendered[0] != "cached non-tool sentinel" {
		t.Error("non-tool cached render was replaced")
	}
	firstRendered := m.rendered[len(m.rendered)-2]
	secondRendered := m.rendered[len(m.rendered)-1]
	if strings.Count(firstRendered, "x") != len(firstOutput) {
		t.Error("older tool did not render its full output after global toggle")
	}
	if strings.Count(secondRendered, "z") != len(secondOutput) {
		t.Error("newer tool did not render its full output after global toggle")
	}
}

func TestToggleToolOutputBeforeToolAppliesToFutureOutput(t *testing.T) {
	m := newLaidOutModel()
	tm, cmd := m.toggleToolOutput()
	m = tm.(Model)
	if cmd != nil {
		t.Errorf("cmd = %v, want nil", cmd)
	}
	if m.toolOutputMode != ui.ToolOutputFull {
		t.Fatalf("mode = %v, want full", m.toolOutputMode)
	}
	m.applyEvent(cake.FunctionCall{ID: "fc-1", CallID: "c-1", Name: "bash", Arguments: `{"command":"make"}`})
	m.applyEvent(cake.FunctionCallOutput{CallID: "c-1", Output: strings.Repeat("x", ui.DefaultOutputLimit+100)})
	if strings.Contains(m.rendered[len(m.rendered)-1], "truncated") {
		t.Error("tool output added after toggle did not use full mode")
	}
}

func TestToggleToolOutputPreservesViewportOffset(t *testing.T) {
	m := New(Config{})
	m.width, m.height = 40, 6
	m.layout()

	// Fill the timeline so it is scrollable.
	for range 10 {
		m.applyEvent(cake.Message{Role: "assistant", Content: "line one\nline two\nline three"})
	}
	m.applyEvent(cake.FunctionCall{ID: "fc-1", CallID: "c-1", Name: "bash", Arguments: `{"command":"make"}`})
	m.applyEvent(cake.FunctionCallOutput{CallID: "c-1", Output: strings.Repeat("x\n", 100)})

	// Scroll up away from the bottom.
	m.timeline.GotoBottom()
	m.timeline.SetYOffset(5)
	wantOffset := m.timeline.YOffset

	tm, _ := m.toggleToolOutput()
	m = tm.(Model)
	if m.timeline.YOffset != wantOffset {
		t.Errorf("viewport offset changed: got %d, want %d", m.timeline.YOffset, wantOffset)
	}
	assertCacheMatchesFullRender(t, &m, "after toggle with preserved offset")
}

func TestToggleToolOutputPreservesBottomPinning(t *testing.T) {
	m := New(Config{})
	m.width, m.height = 40, 6
	m.layout()

	for range 10 {
		m.applyEvent(cake.Message{Role: "assistant", Content: "line one\nline two\nline three"})
	}
	m.applyEvent(cake.FunctionCall{ID: "fc-1", CallID: "c-1", Name: "bash", Arguments: `{"command":"make"}`})
	m.applyEvent(cake.FunctionCallOutput{CallID: "c-1", Output: strings.Repeat("x\n", 100)})
	m.timeline.GotoBottom()

	tm, _ := m.toggleToolOutput()
	m = tm.(Model)
	if !m.timeline.AtBottom() {
		t.Fatal("global tool output toggle did not preserve bottom pinning")
	}
	assertCacheMatchesFullRender(t, &m, "after pinned global toggle")
}

func TestHandleKeyToggleToolOutput(t *testing.T) {
	m := newLaidOutModel()
	m.applyEvent(cake.FunctionCall{ID: "fc-1", CallID: "c-1", Name: "bash", Arguments: `{"command":"make"}`})
	m.applyEvent(cake.FunctionCallOutput{CallID: "c-1", Output: "out"})

	if !key.Matches(tea.KeyMsg{Type: tea.KeyCtrlO}, m.keys.ToggleToolOutput) {
		t.Fatal("ctrl+o binding does not match ctrl+o key msg")
	}

	tm, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlO})
	got := tm.(Model)
	if got.toolOutputMode != ui.ToolOutputFull {
		t.Errorf("handleKey ctrl+o did not toggle to full: mode = %v", got.toolOutputMode)
	}
}

func TestHandleKeyNewSessionResetsConversationAndPreservesLocalState(t *testing.T) {
	m := newLaidOutModel()
	m.cfg.Model = "gpt-x"
	m.cfg.Profile = "work"
	m.session.OnTaskStart(cake.TaskStart{SessionID: "s-1", TaskID: "t-1"})
	m.session.OnTaskComplete(success("s-1"))
	m.history.Add("earlier prompt")
	m.input.SetValue("draft prompt")
	m.applyEvent(cake.FunctionCall{CallID: "call-1", Name: "bash", Arguments: `{}`})

	if !key.Matches(tea.KeyMsg{Type: tea.KeyCtrlN}, m.keys.NewSession) {
		t.Fatal("ctrl+n binding does not match ctrl+n key msg")
	}
	tm, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlN})
	got := tm.(Model)

	if cmd != nil {
		t.Errorf("cmd = %v, want nil", cmd)
	}
	if mode, id := got.session.RunOptions(); mode != cake.RunFresh || id != "" {
		t.Errorf("new session mode=%v id=%q, want fresh with no id", mode, id)
	}
	if got.session.SessionID != "" || got.session.TaskID != "" || got.session.LastComplete != nil {
		t.Errorf("old session state survived: %+v", got.session)
	}
	if len(got.items) != 1 || got.items[0].Kind != ui.KindInfo || got.items[0].Text != "New session" {
		t.Fatalf("timeline = %#v, want one New session info item", got.items)
	}
	if len(got.pendingCalls) != 0 {
		t.Errorf("pending calls survived: %#v", got.pendingCalls)
	}
	if len(got.history.entries) != 1 || got.history.entries[0] != "earlier prompt" {
		t.Errorf("history = %#v, want earlier prompt preserved", got.history.entries)
	}
	if got.input.Value() != "draft prompt" {
		t.Errorf("input = %q, want draft preserved", got.input.Value())
	}
	if got.cfg.Model != "gpt-x" || got.cfg.Profile != "work" {
		t.Errorf("settings changed: model=%q profile=%q", got.cfg.Model, got.cfg.Profile)
	}
	assertCacheMatchesFullRender(t, &got, "after new session")
}

func TestNewSessionDuringRunSuppressesOldRunEventsAndCancelState(t *testing.T) {
	m := newLaidOutModel()
	m.running = true
	m.run = &cake.Run{} // Cancel on a zero Run is a safe no-op.
	m.session.OnTaskStart(cake.TaskStart{SessionID: "old-session", TaskID: "old-task"})

	tm, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlN})
	m = tm.(Model)
	if cmd != nil {
		t.Errorf("cmd = %v, want nil", cmd)
	}
	if !m.newSessionPending {
		t.Fatal("old run was not marked for draining")
	}

	// A final old-run event can race with cancellation. Update must drain it
	// without adding it to the fresh timeline.
	tm, _ = m.Update(eventMsg{ev: cake.Message{Role: "assistant", Content: "late old output"}})
	m = tm.(Model)
	if len(m.items) != 1 || m.items[0].Text != "New session" {
		t.Fatalf("late event crossed boundary: %#v", m.items)
	}

	tm, cmd = m.finishRun(cake.Result{Canceled: true})
	m = tm.(Model)
	if cmd != nil {
		t.Errorf("cmd = %v, want nil", cmd)
	}
	if m.running || m.newSessionPending {
		t.Errorf("run state not settled: running=%v pending=%v", m.running, m.newSessionPending)
	}
	if mode, id := m.session.RunOptions(); mode != cake.RunFresh || id != "" {
		t.Errorf("old cancellation re-pinned session: mode=%v id=%q", mode, id)
	}
	if len(m.items) != 1 || m.items[0].Text != "New session" {
		t.Errorf("cancel result crossed boundary: %#v", m.items)
	}
}

func TestSubmitEmptyInputIsNoop(t *testing.T) {
	m := newLaidOutModel()
	before := len(m.items)
	m.input.SetValue("   \n ")
	tm, cmd := m.submit()
	got := tm.(Model)
	if len(got.items) != before {
		t.Errorf("empty submit appended items: %d -> %d", before, len(got.items))
	}
	if cmd != nil {
		t.Errorf("cmd = %v, want nil", cmd)
	}
}

func TestSubmitDispatchesSlashCommand(t *testing.T) {
	m := newLaidOutModel()
	m.input.SetValue("/help")
	tm, _ := m.submit()
	got := tm.(Model)
	it := lastItem(t, got)
	if it.Kind != ui.KindInfo || it.Text != HelpText {
		t.Errorf("got kind=%v text=%q, want help text info item", it.Kind, it.Text)
	}
	if got.input.Value() != "" {
		t.Error("input not reset after a command")
	}
}

func TestSubmitReportsCommandParseError(t *testing.T) {
	m := newLaidOutModel()
	m.input.SetValue("/bogus")
	tm, _ := m.submit()
	got := tm.(Model)
	it := lastItem(t, got)
	if it.Kind != ui.KindError || !strings.Contains(it.Text, "unknown command") {
		t.Errorf("got kind=%v text=%q, want unknown-command error", it.Kind, it.Text)
	}
	if got.input.Value() != "" {
		t.Error("input not reset after an invalid command")
	}
}

func TestSubmitWhileRunningWarnsAndKeepsInput(t *testing.T) {
	m := newLaidOutModel()
	m.running = true
	m.input.SetValue("second prompt")
	tm, cmd := m.submit()
	got := tm.(Model)
	it := lastItem(t, got)
	if it.Kind != ui.KindWarning || !strings.Contains(it.Text, "already running") {
		t.Errorf("got kind=%v text=%q, want already-running warning", it.Kind, it.Text)
	}
	// The prompt stays in the input so it can be resubmitted after cancel.
	if got.input.Value() != "second prompt" {
		t.Errorf("input = %q, want the typed prompt kept", got.input.Value())
	}
	if cmd != nil {
		t.Errorf("cmd = %v, want nil", cmd)
	}
}

func TestSubmitStartFailureLandsOnTimeline(t *testing.T) {
	m := newLaidOutModel()
	m.cfg.CakeBin = filepath.Join(t.TempDir(), "missing-cake")
	m.input.SetValue("hello")
	tm, cmd := m.submit()
	got := tm.(Model)
	if got.running {
		t.Error("failed start left the model running")
	}
	it := lastItem(t, got)
	if it.Kind != ui.KindError || !strings.Contains(it.Text, "missing-cake") {
		t.Errorf("got kind=%v text=%q, want start error naming the binary", it.Kind, it.Text)
	}
	// The prompt stays in the input so it survives a fixable config error.
	if got.input.Value() != "hello" {
		t.Errorf("input = %q, want the typed prompt kept", got.input.Value())
	}
	if cmd != nil {
		t.Errorf("cmd = %v, want nil", cmd)
	}
}

func TestSubmitStartsRun(t *testing.T) {
	m := newLaidOutModel()
	m.cfg.CakeBin = writeFakeCake(t, "exit 0\n")
	m.sawComplete = true // stale value from a previous run must reset
	m.input.SetValue("do the thing")
	tm, cmd := m.submit()
	got := tm.(Model)
	if !got.running || got.run == nil {
		t.Fatal("submit did not start a run")
	}
	t.Cleanup(func() { drainRun(t, got.run) })
	it := lastItem(t, got)
	if it.Kind != ui.KindUser || it.Text != "do the thing" {
		t.Errorf("got kind=%v text=%q, want the prompt as a user item", it.Kind, it.Text)
	}
	if got.input.Value() != "" {
		t.Error("input not reset after starting a run")
	}
	if got.sawComplete {
		t.Error("sawComplete not reset at run start")
	}
	if cmd == nil {
		t.Error("expected spinner tick and run wait commands")
	}
}

func TestSubmitRecordsHistory(t *testing.T) {
	m := newLaidOutModel()
	m.cfg.CakeBin = writeFakeCake(t, "exit 0\n")
	m.input.SetValue("do the thing")
	tm, _ := m.submit()
	got := tm.(Model)
	t.Cleanup(func() { drainRun(t, got.run) })
	if len(got.history.entries) != 1 || got.history.entries[0] != "do the thing" {
		t.Errorf("history = %v, want the submitted prompt", got.history.entries)
	}

	got.input.SetValue("/help")
	tm, _ = got.submit()
	got = tm.(Model)
	if entries := got.history.entries; len(entries) != 2 || entries[1] != "/help" {
		t.Errorf("history = %v, want the command recorded too", entries)
	}
}

func TestSubmitStartFailureNotRecordedInHistory(t *testing.T) {
	m := newLaidOutModel()
	m.cfg.CakeBin = filepath.Join(t.TempDir(), "missing-cake")
	m.input.SetValue("hello")
	tm, _ := m.submit()
	got := tm.(Model)
	if len(got.history.entries) != 0 {
		t.Errorf("history = %v, want empty — the prompt is still in the input", got.history.entries)
	}
}

func TestHandleKeyHistoryRecall(t *testing.T) {
	m := newLaidOutModel()
	m.history.Add("first prompt")
	m.history.Add("/help")
	up := tea.KeyMsg{Type: tea.KeyUp}
	down := tea.KeyMsg{Type: tea.KeyDown}

	m.input.SetValue("draft")
	steps := []struct {
		key  tea.KeyMsg
		want string
	}{
		{up, "/help"},
		{up, "first prompt"},
		{up, "first prompt"}, // at the oldest entry the input is left alone
		{down, "/help"},
		{down, "draft"}, // walking back down restores the draft
	}
	for i, s := range steps {
		tm, _ := m.handleKey(s.key)
		m = tm.(Model)
		if got := m.input.Value(); got != s.want {
			t.Fatalf("step %d: input = %q, want %q", i, got, s.want)
		}
	}
}

func TestHandleKeyHistoryOnlyAtInputEdges(t *testing.T) {
	m := newLaidOutModel()
	m.history.Add("old prompt")

	// The cursor starts on the last line; up must move the cursor, not recall.
	m.input.SetValue("line one\nline two")
	tm, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyUp})
	m = tm.(Model)
	if m.input.Value() != "line one\nline two" {
		t.Fatalf("up mid-input replaced the value: %q", m.input.Value())
	}

	// Now on the first line but not the last; down must not recall either.
	tm, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyDown})
	m = tm.(Model)
	if m.input.Value() != "line one\nline two" {
		t.Fatalf("down mid-input replaced the value: %q", m.input.Value())
	}

	// Back on the first line; up recalls.
	tm, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyUp})
	m = tm.(Model)
	tm, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyUp})
	m = tm.(Model)
	if m.input.Value() != "old prompt" {
		t.Errorf("input = %q, want recalled entry", m.input.Value())
	}
}

func TestExecCommandHelp(t *testing.T) {
	m := newLaidOutModel()
	tm, cmd := m.execCommand(Command{Kind: CmdHelp})
	got := tm.(Model)
	it := lastItem(t, got)
	if it.Kind != ui.KindInfo || it.Text != HelpText {
		t.Errorf("got kind=%v text=%q, want help text info item", it.Kind, it.Text)
	}
	if cmd != nil {
		t.Errorf("cmd = %v, want nil", cmd)
	}
}

func TestExecCommandExitQuitsWhenIdle(t *testing.T) {
	m := newLaidOutModel()
	_, cmd := m.execCommand(Command{Kind: CmdExit})
	if cmd == nil {
		t.Fatal("no command returned")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Errorf("cmd() = %T, want tea.QuitMsg", cmd())
	}
}

func TestExecCommandExitWhileRunningCancelsFirst(t *testing.T) {
	m := newLaidOutModel()
	m.running = true
	m.run = &cake.Run{} // Cancel on a zero Run is a safe no-op
	tm, cmd := m.execCommand(Command{Kind: CmdExit})
	got := tm.(Model)
	if !got.exitAfter {
		t.Error("exitAfter not set")
	}
	if cmd != nil {
		t.Errorf("cmd = %v, want nil — quitting happens in finishRun", cmd)
	}
	it := lastItem(t, got)
	if it.Kind != ui.KindWarning || !strings.Contains(it.Text, "exiting") {
		t.Errorf("got kind=%v text=%q, want canceling-then-exiting warning", it.Kind, it.Text)
	}
}

func TestFinishRunCanceledPinsToStartedSession(t *testing.T) {
	m := newLaidOutModel()
	m.running = true
	m.session.OnTaskStart(cake.TaskStart{SessionID: "d8fceb36", TaskID: "t-1"})
	tm, cmd := m.finishRun(cake.Result{Canceled: true})
	m = tm.(Model)
	if cmd != nil {
		t.Errorf("cmd = %v, want nil", cmd)
	}
	mode, resumeID := m.session.RunOptions()
	if mode != cake.RunResume || resumeID != "d8fceb36" {
		t.Errorf("after cancel mode=%v id=%q, want resume pinned to d8fceb36", mode, resumeID)
	}
	it := lastItem(t, m)
	if it.Kind != ui.KindWarning || it.Text != "canceled" {
		t.Errorf("last item kind=%v text=%q, want canceled warning", it.Kind, it.Text)
	}
}

func TestFinishRunCanceledBeforeTaskStartDoesNotInventSession(t *testing.T) {
	m := newLaidOutModel()
	m.running = true
	tm, cmd := m.finishRun(cake.Result{Canceled: true})
	m = tm.(Model)
	if cmd != nil {
		t.Errorf("cmd = %v, want nil", cmd)
	}
	mode, resumeID := m.session.RunOptions()
	if mode != cake.RunFresh || resumeID != "" {
		t.Errorf("after cancel mode=%v id=%q, want fresh with no id", mode, resumeID)
	}
}

func TestFinishRunQuitsAfterExitCommand(t *testing.T) {
	m := newLaidOutModel()
	m.running = true
	m.exitAfter = true
	_, cmd := m.finishRun(cake.Result{Canceled: true})
	if cmd == nil {
		t.Fatal("no command returned")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Errorf("cmd() = %T, want tea.QuitMsg", cmd())
	}
}

func TestExecSessionCommandsRejectedWhenRunning(t *testing.T) {
	const activeID = "11111111-2222-3333-4444-555555555555"
	tests := []struct {
		name  string
		input string
	}{
		{"new", "/new"},
		{"continue", "/continue"},
		{"resume", "/resume aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newLaidOutModel()
			m.running = true
			m.session.OnTaskStart(cake.TaskStart{SessionID: activeID, TaskID: "t-1"})
			modeBefore, resumeBefore := m.session.RunOptions()

			m.input.SetValue(tt.input)
			tm, cmd := m.submit()
			m = tm.(Model)
			if cmd != nil {
				t.Errorf("cmd = %v, want nil", cmd)
			}
			it := lastItem(t, m)
			if it.Kind != ui.KindWarning || !strings.Contains(it.Text, "finish or cancel the running task first") {
				t.Errorf("got kind=%v text=%q, want rejection warning", it.Kind, it.Text)
			}
			if mode, resumeID := m.session.RunOptions(); mode != modeBefore || resumeID != resumeBefore {
				t.Errorf("session mode changed from (%v,%q) to (%v,%q)", modeBefore, resumeBefore, mode, resumeID)
			}

			m.applyEvent(success(activeID))
			tm, cmd = m.finishRun(cake.Result{ExitCode: 0})
			m = tm.(Model)
			if cmd != nil {
				t.Errorf("finish cmd = %v, want nil", cmd)
			}
			if mode, resumeID := m.session.RunOptions(); mode != cake.RunResume || resumeID != activeID {
				t.Errorf("after success mode=%v id=%q, want resume pinned to %q", mode, resumeID, activeID)
			}
		})
	}
}

func TestExecCommandSessionModes(t *testing.T) {
	const id = "11111111-2222-3333-4444-555555555555"
	m := newLaidOutModel()
	m.session.OnTaskStart(cake.TaskStart{SessionID: id, TaskID: "t-1"})

	tm, _ := m.execCommand(Command{Kind: CmdResume, Arg: id})
	got := tm.(Model)
	if mode, resumeID := got.session.RunOptions(); mode != cake.RunResume || resumeID != id {
		t.Errorf("after /resume: mode=%v id=%q, want resume %q", mode, resumeID, id)
	}

	tm, _ = got.execCommand(Command{Kind: CmdContinue})
	got = tm.(Model)
	if mode, resumeID := got.session.RunOptions(); mode != cake.RunContinue || resumeID != "" {
		t.Errorf("after /continue: mode=%v id=%q, want continue with no id", mode, resumeID)
	}

	tm, _ = got.execCommand(Command{Kind: CmdNew})
	got = tm.(Model)
	if mode, _ := got.session.RunOptions(); mode != cake.RunFresh {
		t.Errorf("after /new: mode=%v, want fresh", mode)
	}
	if got.session.SessionID != "" {
		t.Error("/new should drop the tracked session id")
	}
}

func TestExecCommandSessionInfo(t *testing.T) {
	m := newLaidOutModel()
	m.cfg.Cwd = "/tmp/proj"
	m.cfg.Model = "gpt-x"
	m.session.OnTaskStart(cake.TaskStart{SessionID: "11111111-2222-3333-4444-555555555555", TaskID: "t-1"})

	tm, _ := m.execCommand(Command{Kind: CmdSession})
	got := tm.(Model)
	it := lastItem(t, got)
	if it.Kind != ui.KindInfo {
		t.Fatalf("kind = %v, want info", it.Kind)
	}
	for _, want := range []string{
		"session:  11111111-2222-3333-4444-555555555555",
		"task:     t-1",
		"cwd:      /tmp/proj",
		"next run: fresh",
		"model:    gpt-x",
	} {
		if !strings.Contains(it.Text, want) {
			t.Errorf("session info missing %q:\n%s", want, it.Text)
		}
	}
}

// Cake streams each message exactly once, when its turn completes (verified
// against cake's source: items are emitted per finished turn, and no
// in-progress message records exist). The model therefore appends messages
// without deduplicating by ID; these tests pin the filtering that does
// happen per event.
func TestHumanDuration(t *testing.T) {
	tests := []struct {
		ms   int64
		want string
	}{
		{0, "0ms"},
		{50, "50ms"},
		{500, "500ms"},
		{1500, "1.5s"},
		{59000, "59.0s"},
		{60000, "1m00s"},
		{90000, "1m30s"},
		{3600000, "1h00m00s"},
		{3661000, "1h01m01s"},
		{7200000, "2h00m00s"},
		{73845000, "20h30m45s"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := humanDuration(tt.ms)
			if got != tt.want {
				t.Errorf("humanDuration(%d) = %q, want %q", tt.ms, got, tt.want)
			}
		})
	}
}

func TestApplyEventMessages(t *testing.T) {
	m := newLaidOutModel()
	base := len(m.items)

	m.applyEvent(cake.Message{Role: "assistant", Content: "an answer", ID: "msg-1", Status: "completed"})
	if len(m.items) != base+1 {
		t.Fatalf("assistant message appended %d items, want 1", len(m.items)-base)
	}
	it := lastItem(t, m)
	if it.Kind != ui.KindAssistant || it.Text != "an answer" {
		t.Errorf("got kind=%v text=%q, want assistant item", it.Kind, it.Text)
	}

	m.applyEvent(cake.Message{Role: "assistant", Content: " \n\t"})
	if len(m.items) != base+1 {
		t.Error("whitespace-only assistant message should be skipped")
	}

	m.applyEvent(cake.Message{Role: "user", Content: "the prompt"})
	if len(m.items) != base+1 {
		t.Error("user message should be skipped — the prompt is already on the timeline")
	}

	m.applyEvent(cake.Message{Role: "developer", Content: "instructions"})
	if len(m.items) != base+1 {
		t.Error("other-role message should be skipped without a debug log")
	}

	var debug bytes.Buffer
	m.cfg.DebugLog = &debug
	m.applyEvent(cake.Message{Role: "developer", Content: "instructions"})
	if len(m.items) != base+1 {
		t.Error("other-role message should stay off the timeline even with a debug log")
	}
	if got := debug.String(); !strings.Contains(got, "developer message: instructions") {
		t.Errorf("debug log missing developer message, got %q", got)
	}
	assertCacheMatchesFullRender(t, &m, "after message events")
}

func TestApplyEventReordersAssistantMessageBeforePendingTool(t *testing.T) {
	// Simulate the Chat Completions backend bug: function_call events arrive
	// before the assistant message event. The assistant text should appear
	// BEFORE the tool call in the timeline.
	m := newLaidOutModel()

	// Step 1: function_call arrives first (wrong order from Chat Completions backend)
	m.applyEvent(cake.FunctionCall{ID: "fc-1", CallID: "c-1", Name: "bash", Arguments: `{"command":"ls"}`})
	if len(m.items) != 2 { // info + tool
		t.Fatalf("after function_call: got %d items, want 2", len(m.items))
	}
	if m.items[1].Kind != ui.KindTool {
		t.Fatalf("item[1].Kind = %v, want KindTool", m.items[1].Kind)
	}
	assertCacheMatchesFullRender(t, &m, "after function_call")

	// Step 2: assistant message arrives after the tool call
	m.applyEvent(cake.Message{Role: "assistant", Content: "I'll search for the file."})
	if len(m.items) != 3 { // info + assistant + tool
		t.Fatalf("after message: got %d items, want 3", len(m.items))
	}
	// Message should now be BEFORE the tool call
	if m.items[1].Kind != ui.KindAssistant {
		t.Errorf("item[1].Kind = %v, want KindAssistant (should be before tool)", m.items[1].Kind)
	}
	if m.items[1].Text != "I'll search for the file." {
		t.Errorf("item[1].Text = %q, want assistant text", m.items[1].Text)
	}
	if m.items[2].Kind != ui.KindTool {
		t.Errorf("item[2].Kind = %v, want KindTool (should be after message)", m.items[2].Kind)
	}
	if idx, ok := m.pendingCalls["c-1"]; !ok || idx != 2 {
		t.Errorf("pendingCalls['c-1'] = %d (ok=%v), want 2", idx, ok)
	}
	assertCacheMatchesFullRender(t, &m, "after message before pending tool")

	// Step 3: tool output still correctly updates the tool at its new position
	m.applyEvent(cake.FunctionCallOutput{CallID: "c-1", Output: "file.txt"})
	if m.items[2].Kind != ui.KindTool {
		t.Fatalf("after output: item[2].Kind = %v, want KindTool", m.items[2].Kind)
	}
	if !m.items[2].Tool.Done {
		t.Error("tool should be done after receiving output")
	}
	if m.items[2].Tool.Output != "file.txt" {
		t.Errorf("tool output = %q, want 'file.txt'", m.items[2].Tool.Output)
	}
	assertCacheMatchesFullRender(t, &m, "after tool output")
}

func TestApplyEventAppendsAssistantMessageWhenNoPendingTool(t *testing.T) {
	// When there are no pending tool calls, the message should append normally.
	m := newLaidOutModel()
	m.applyEvent(cake.Message{Role: "assistant", Content: "hello"})
	if len(m.items) != 2 { // info + assistant
		t.Fatalf("got %d items, want 2", len(m.items))
	}
	if m.items[1].Kind != ui.KindAssistant || m.items[1].Text != "hello" {
		t.Errorf("item[1] = %+v, want assistant message", m.items[1])
	}
	assertCacheMatchesFullRender(t, &m, "after assistant message")
}

func TestApplyEventReordersAssistantBeforeMultiplePendingTools(t *testing.T) {
	// Multiple pending tool calls: the assistant message should be inserted
	// before ALL of them, not just the first one.
	m := newLaidOutModel()

	m.applyEvent(cake.FunctionCall{ID: "fc-1", CallID: "c-1", Name: "bash", Arguments: `{"command":"ls"}`})
	m.applyEvent(cake.FunctionCall{ID: "fc-2", CallID: "c-2", Name: "read", Arguments: `{"path":"main.go"}`})
	// At this point: info, tool(c-1), tool(c-2)

	m.applyEvent(cake.Message{Role: "assistant", Content: "Let me check both files."})
	if len(m.items) != 4 { // info + assistant + tool + tool
		t.Fatalf("got %d items, want 4", len(m.items))
	}
	// Message should be at index 1, before both tools
	if m.items[1].Kind != ui.KindAssistant || m.items[1].Text != "Let me check both files." {
		t.Errorf("item[1] = %+v, want assistant message before tools", m.items[1])
	}
	if m.items[2].Kind != ui.KindTool || m.items[3].Kind != ui.KindTool {
		t.Errorf("items[2,3] should be tools, got kinds %v, %v", m.items[2].Kind, m.items[3].Kind)
	}
	// Both pending call indices should be updated
	if idx, ok := m.pendingCalls["c-1"]; !ok || idx != 2 {
		t.Errorf("pendingCalls['c-1'] = %d (ok=%v), want 2", idx, ok)
	}
	if idx, ok := m.pendingCalls["c-2"]; !ok || idx != 3 {
		t.Errorf("pendingCalls['c-2'] = %d (ok=%v), want 3", idx, ok)
	}
	assertCacheMatchesFullRender(t, &m, "after message before multiple pending tools")
}

func TestApplyEventPreservesOrderWhenMessageBeforeTool(t *testing.T) {
	// When cake emits events in the correct order (message before function_call),
	// the message should still be before the tool call.
	m := newLaidOutModel()

	m.applyEvent(cake.Message{Role: "assistant", Content: "I'll search for it."})
	m.applyEvent(cake.FunctionCall{ID: "fc-1", CallID: "c-1", Name: "bash", Arguments: `{"command":"find ."}`})

	if len(m.items) != 3 { // info + assistant + tool
		t.Fatalf("got %d items, want 3", len(m.items))
	}
	if m.items[1].Kind != ui.KindAssistant || m.items[1].Text != "I'll search for it." {
		t.Errorf("item[1] = %+v, want assistant message", m.items[1])
	}
	if m.items[2].Kind != ui.KindTool {
		t.Errorf("item[2].Kind = %v, want KindTool", m.items[2].Kind)
	}
	if idx, ok := m.pendingCalls["c-1"]; !ok || idx != 2 {
		t.Errorf("pendingCalls['c-1'] = %d (ok=%v), want 2", idx, ok)
	}
	assertCacheMatchesFullRender(t, &m, "after correct-order events")
}

func TestInsertItemAtDoesNotPanicOnEmptyTimeline(t *testing.T) {
	var m Model
	m.pendingCalls = map[string]int{}
	// insertItemAt on an empty model must not panic.
	m.insertItemAt(0, ui.Item{Kind: ui.KindAssistant, Text: "hello"})
	if len(m.items) != 1 || m.items[0].Text != "hello" {
		t.Errorf("insert into empty: got %+v", m.items)
	}
}

func TestFirstPendingToolIdx(t *testing.T) {
	m := newLaidOutModel() // has 1 item (info)

	if idx := m.firstPendingToolIdx(); idx != -1 {
		t.Errorf("empty timeline: got %d, want -1", idx)
	}

	m.applyEvent(cake.FunctionCall{ID: "fc-1", CallID: "c-1", Name: "bash", Arguments: `{}`})
	if idx := m.firstPendingToolIdx(); idx != 1 {
		t.Errorf("single pending tool: got %d, want 1", idx)
	}

	// A completed tool should not be detected as pending.
	m.applyEvent(cake.FunctionCallOutput{CallID: "c-1", Output: "done"})
	if idx := m.firstPendingToolIdx(); idx != -1 {
		t.Errorf("after tool completed: got %d, want -1", idx)
	}
}

func TestInsertItemAtRespectsMaxTimelineItems(t *testing.T) {
	// When MaxTimelineItems is set and the timeline is at its limit,
	// inserting an assistant message before pending tools must not grow
	// beyond the cap, and pendingCalls indices must remain valid.
	m := newLaidOutModel()
	m.cfg.MaxTimelineItems = 3

	// Fill the timeline to the cap: info + assistant + tool
	m.applyEvent(cake.Message{Role: "assistant", Content: "first"})
	m.applyEvent(cake.FunctionCall{ID: "fc-1", CallID: "c-1", Name: "bash", Arguments: `{"command":"ls"}`})
	if len(m.items) != 3 {
		t.Fatalf("before insert: got %d items, want 3", len(m.items))
	}

	// Now insert a second assistant message before the pending tool.
	// This would push items to 4, triggering the trim to 3.
	m.applyEvent(cake.Message{Role: "assistant", Content: "Let me check."})
	if len(m.items) != m.cfg.MaxTimelineItems {
		t.Errorf("after insert: got %d items, want %d", len(m.items), m.cfg.MaxTimelineItems)
	}

	// The initial info message should have been trimmed.
	// Surviving items: second assistant, first assistant?, or assistant + tool?
	// After insertion: items were [info, a1, tool], insert at idx=1 → [info, a2, a1, tool],
	// then trim front by 1 → [a2, a1, tool]. But actually, after first_pending_tool_idx:
	//   items = [info, a1, tool], pending tool at idx=2
	//   firstPendingToolIdx() → 2
	//   insert at 2 → [info, a1, a2, tool]
	//   then trim front by 1 → [a1, a2, tool]
	// So surviving: assistant "first", assistant "Let me check.", tool
	if m.items[0].Kind != ui.KindAssistant || m.items[0].Text != "first" {
		t.Errorf("item[0] = %+v, want assistant 'first'", m.items[0])
	}
	if m.items[1].Kind != ui.KindAssistant || m.items[1].Text != "Let me check." {
		t.Errorf("item[1] = %+v, want assistant 'Let me check.'", m.items[1])
	}
	if m.items[2].Kind != ui.KindTool {
		t.Errorf("item[2].Kind = %v, want KindTool", m.items[2].Kind)
	}

	// Pending call index should still point to the tool at its new position.
	if idx, ok := m.pendingCalls["c-1"]; !ok || idx != 2 {
		t.Errorf("pendingCalls['c-1'] = %d (ok=%v), want 2", idx, ok)
	}

	// Tool output should still update correctly.
	m.applyEvent(cake.FunctionCallOutput{CallID: "c-1", Output: "files"})
	if !m.items[2].Tool.Done || m.items[2].Tool.Output != "files" {
		t.Errorf("tool after output: done=%v output=%q", m.items[2].Tool.Done, m.items[2].Tool.Output)
	}

	assertCacheMatchesFullRender(t, &m, "after cap-enforced insert and tool output")
}

func TestInsertItemAtTrimsPendingCallsInFrontRange(t *testing.T) {
	// When the front-trim removes a tool that was still pending, its
	// pendingCalls entry should be removed, not left stale.
	m := newLaidOutModel()
	m.cfg.MaxTimelineItems = 2

	// Fill to cap: info + tool(c-1) = 2 items, at cap.
	m.applyEvent(cake.FunctionCall{ID: "fc-1", CallID: "c-1", Name: "bash", Arguments: `{}`})
	if len(m.items) != 2 {
		t.Fatalf("at cap: got %d items, want 2", len(m.items))
	}
	if _, ok := m.pendingCalls["c-1"]; !ok {
		t.Fatal("pendingCalls missing c-1 after append")
	}

	// Inserting a message before the tool pushes items to 3, triggers trim
	// of the oldest (info at index 0), then items are [message, tool] at cap.
	m.applyEvent(cake.Message{Role: "assistant", Content: "checking"})
	if len(m.items) != m.cfg.MaxTimelineItems {
		t.Fatalf("after insert+trim: got %d items, want %d", len(m.items), m.cfg.MaxTimelineItems)
	}

	// c-1 should still be in pendingCalls and point to the tool.
	if idx, ok := m.pendingCalls["c-1"]; !ok || idx != 1 {
		t.Errorf("pendingCalls['c-1'] = %d (ok=%v), want 1", idx, ok)
	}

	assertCacheMatchesFullRender(t, &m, "after cap-enforced insert trims front")
}

func TestAppendItemUpdatesPendingCallsOnTrim(t *testing.T) {
	// appendItem must adjust pendingCalls indices when MaxTimelineItems
	// trimming shifts items left; otherwise a FunctionCallOutput resolves
	// to the wrong index, writing output into a different pending tool's
	// block.
	m := newLaidOutModel()
	m.cfg.MaxTimelineItems = 3

	// info item fills slot 0; add two tools to reach the cap.
	m.applyEvent(cake.FunctionCall{ID: "fc-1", CallID: "A", Name: "bash", Arguments: `{"command":"echo a"}`})
	m.applyEvent(cake.FunctionCall{ID: "fc-2", CallID: "B", Name: "bash", Arguments: `{"command":"echo b"}`})
	// items: [info, tool-A, tool-B] ← at cap (3)

	// A reasoning item pushes to 4, triggers trim of the front.
	// After trim: [tool-A, tool-B, reasoning]
	m.applyEvent(cake.Reasoning{Summary: []string{"thinking"}})

	// pendingCalls must reflect the left shift: A→0, B→1
	if idx, ok := m.pendingCalls["A"]; !ok || idx != 0 {
		t.Errorf("pendingCalls['A'] = %d (ok=%v), want 0", idx, ok)
	}
	if idx, ok := m.pendingCalls["B"]; !ok || idx != 1 {
		t.Errorf("pendingCalls['B'] = %d (ok=%v), want 1", idx, ok)
	}

	// Output for A must go to tool A's block, not B's.
	m.applyEvent(cake.FunctionCallOutput{CallID: "A", Output: "OUTPUT-A"})
	if !m.items[0].Tool.Done || m.items[0].Tool.Output != "OUTPUT-A" {
		t.Errorf("tool A after output: done=%v output=%q", m.items[0].Tool.Done, m.items[0].Tool.Output)
	}
	// B must still be pending.
	if m.items[1].Tool.Done {
		t.Error("tool B should still be pending")
	}

	// A's pending entry must be gone (consumed by output).
	if _, ok := m.pendingCalls["A"]; ok {
		t.Error("pendingCalls should not contain A after its output arrived")
	}

	assertCacheMatchesFullRender(t, &m, "after trim and tool A output")
}

func TestAppendItemRemovesPendingCallWhenToolTrimmedAway(t *testing.T) {
	// When a pending tool call's item is itself trimmed out of the
	// timeline window, its pendingCalls entry must be deleted so that
	// late output falls back to a standalone block instead of writing
	// into a different tool's block.
	m := newLaidOutModel()
	m.cfg.MaxTimelineItems = 2

	// items: [info, tool-A]
	m.applyEvent(cake.FunctionCall{ID: "fc-1", CallID: "A", Name: "bash", Arguments: `{"command":"echo a"}`})

	// items: [tool-A, tool-B] (info trimmed away)
	m.applyEvent(cake.FunctionCall{ID: "fc-2", CallID: "B", Name: "bash", Arguments: `{"command":"echo b"}`})

	// items: [tool-B, reasoning] (tool-A trimmed away)
	m.applyEvent(cake.Reasoning{Summary: []string{"thinking"}})

	// A must be gone from pendingCalls since its item was trimmed.
	if _, ok := m.pendingCalls["A"]; ok {
		t.Error("pendingCalls should not contain trimmed-away call A")
	}
	// B must be at the new correct index.
	if idx, ok := m.pendingCalls["B"]; !ok || idx != 0 {
		t.Errorf("pendingCalls['B'] = %d (ok=%v), want 0", idx, ok)
	}

	// Late output for A must append a standalone block. The append may
	// trigger another front-trim that removes B's item, which is expected.
	m.applyEvent(cake.FunctionCallOutput{CallID: "A", Output: "LATE-A"})
	if len(m.items) != m.cfg.MaxTimelineItems {
		t.Errorf("items len = %d, want %d (after trim to cap)", len(m.items), m.cfg.MaxTimelineItems)
	}
	// The last item is the newest (standalone tool output survived the
	// front-trim).
	last := m.items[len(m.items)-1]
	if last.Kind != ui.KindTool || last.Tool.Name != "(tool output)" || last.Tool.Output != "LATE-A" {
		t.Errorf("last item: got %+v, want standalone '(tool output)' block", last)
	}

	assertCacheMatchesFullRender(t, &m, "after trimmed tool's late output")
}

func TestPreReadyTrimDoesNotPanic(t *testing.T) {
	// Regression: appending past MaxTimelineItems before the first
	// WindowSizeMsg (when !m.ready) must not panic on the empty render
	// cache. Drive through Update, not by calling appendItem directly.
	m := New(Config{MaxTimelineItems: 3})
	// m.items starts with the welcome message (1 item at index 0).
	// Sending events through Update before any layout exercises the
	// pre-ready path.
	for _, msg := range []tea.Msg{
		eventMsg{cake.TaskStart{SessionID: "11111111-2222-3333-4444-555555555555", TaskID: "t-1"}},
		eventMsg{cake.Message{Role: "assistant", Content: "hello"}},
		eventMsg{cake.FunctionCall{ID: "fc-1", CallID: "c-1", Name: "bash", Arguments: `{}`}},
		eventMsg{cake.FunctionCallOutput{CallID: "c-1", Output: "output"}},
	} {
		tm, _ := m.Update(msg)
		m = tm.(Model)
	}

	// Render cache must be empty before first layout.
	if len(m.rendered) != 0 {
		t.Errorf("before layout: rendered = %d, want 0", len(m.rendered))
	}

	// After layout the cache must reflect the trimmed items.
	m.width, m.height = 80, 24
	m.layout()
	if len(m.rendered) != len(m.items) {
		t.Errorf("after layout: rendered=%d items=%d, want equal", len(m.rendered), len(m.items))
	}
	if len(m.items) > 3 {
		t.Errorf("after layout with max=3: items=%d, want ≤3", len(m.items))
	}
	assertCacheMatchesFullRender(t, &m, "after pre-ready trim then layout")
}

func TestPreReadyTrimWithPendingCalls(t *testing.T) {
	// Same scenario as above but with a mix of tool items whose call_ids
	// must survive the trim.
	m := New(Config{MaxTimelineItems: 3})
	// items: [welcome] → send events before layout
	for _, msg := range []tea.Msg{
		eventMsg{cake.FunctionCall{ID: "fc-1", CallID: "A", Name: "bash", Arguments: `{}`}},
		eventMsg{cake.FunctionCall{ID: "fc-2", CallID: "B", Name: "bash", Arguments: `{}`}},
		// 4th item triggers trim; welcome trimmed away
		eventMsg{cake.Reasoning{Summary: []string{"thinking"}}},
	} {
		tm, _ := m.Update(msg)
		m = tm.(Model)
	}

	// pendingCalls must reflect the left shift.
	if idx, ok := m.pendingCalls["A"]; !ok || idx != 0 {
		t.Errorf("pendingCalls['A'] = %d (ok=%v), want 0", idx, ok)
	}
	if idx, ok := m.pendingCalls["B"]; !ok || idx != 1 {
		t.Errorf("pendingCalls['B'] = %d (ok=%v), want 1", idx, ok)
	}

	// Late output for A must find the right item.
	tm2, _ := m.Update(eventMsg{cake.FunctionCallOutput{CallID: "A", Output: "OUTPUT-A"}})
	m = tm2.(Model)
	if !m.items[0].Tool.Done || m.items[0].Tool.Output != "OUTPUT-A" {
		t.Errorf("tool A after output: done=%v output=%q", m.items[0].Tool.Done, m.items[0].Tool.Output)
	}

	// Layout and verify full render consistency.
	m.width, m.height = 80, 24
	m.layout()
	assertCacheMatchesFullRender(t, &m, "after pre-ready trim with pending calls then layout")
}
