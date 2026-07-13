package app

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
