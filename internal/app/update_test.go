package app

import (
	"io"
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

	m.cfg.DebugLog = io.Discard
	m.applyEvent(cake.Message{Role: "developer", Content: "instructions"})
	if len(m.items) != base+2 {
		t.Fatal("other-role message should surface as debug when a debug log is set")
	}
	it = lastItem(t, m)
	if it.Kind != ui.KindDebug || !strings.Contains(it.Text, "developer message: instructions") {
		t.Errorf("got kind=%v text=%q, want developer debug item", it.Kind, it.Text)
	}
	assertCacheMatchesFullRender(t, &m, "after message events")
}
