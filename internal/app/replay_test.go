package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/travisennis/cake-repl/internal/cake"
	"github.com/travisennis/cake-repl/internal/ui"
)

const replayTestSessionID = "11111111-2222-3333-4444-555555555555"

// hydrateTestModel drives the same Init command and replay message flow that
// Bubble Tea uses at startup. It returns only after replay has finished.
func hydrateTestModel(t *testing.T, m Model) Model {
	t.Helper()
	batch, ok := m.Init()().(tea.BatchMsg)
	if !ok {
		t.Fatalf("Init message type = %T, want tea.BatchMsg", m.Init()())
	}

	var start replayStartedMsg
	var found bool
	for _, cmd := range batch {
		if msg := cmd(); msg != nil {
			if candidate, ok := msg.(replayStartedMsg); ok {
				start = candidate
				found = true
				break
			}
		}
	}
	if !found {
		t.Fatal("Init did not start replay")
	}

	tm, cmd := m.Update(start)
	m = tm.(Model)
	for m.hydrating || m.replayRun != nil {
		if cmd == nil {
			t.Fatalf("replay state still active without a command: %+v", m)
		}
		msg := cmd()
		tm, cmd = m.Update(msg)
		m = tm.(Model)
	}
	return m
}

func collectCakeRunResult(t *testing.T, run *cake.Run) cake.Result {
	t.Helper()
	deadline := time.After(10 * time.Second)
	for {
		select {
		case _, ok := <-run.Events:
			if !ok {
				select {
				case result := <-run.Result:
					return result
				case <-deadline:
					t.Fatal("timed out waiting for cake result")
				}
			}
		case <-deadline:
			t.Fatal("timed out waiting for cake events")
		}
	}
}

func TestResumeStartupHydratesTimelineAndContinuesPinnedPrompt(t *testing.T) {
	argsPath := filepath.Join(t.TempDir(), "args")
	t.Setenv("ARGS_FILE", argsPath)
	bin := writeFakeCake(t, `
printf '%s\n' "$@" >> "$ARGS_FILE"
if [ "$3" = "replay" ]; then
cat <<'EOF'
{"type":"session_meta","session_id":"11111111-2222-3333-4444-555555555555","future_metadata":true}
{"type":"task_start","session_id":"11111111-2222-3333-4444-555555555555","task_id":"old-task"}
{"type":"prompt_context","session_id":"11111111-2222-3333-4444-555555555555","task_id":"old-task","extra":"ignored"}
{"type":"message","role":"user","content":"previous prompt"}
{"type":"message","role":"assistant","content":"previous answer"}
{"type":"function_call","id":"fc-1","call_id":"call-1","name":"bash","arguments":"{\"command\":\"pwd\"}"}
{"type":"function_call_output","call_id":"call-1","output":"/tmp/project"}
{"type":"task_complete","subtype":"success","is_error":false,"session_id":"11111111-2222-3333-4444-555555555555","task_id":"old-task"}
EOF
else
printf '%s\n' '{"type":"task_start","session_id":"11111111-2222-3333-4444-555555555555","task_id":"new-task"}'
printf '%s\n' '{"type":"task_complete","subtype":"success","is_error":false,"session_id":"11111111-2222-3333-4444-555555555555","task_id":"new-task"}'
fi
`)

	m := New(Config{
		CakeBin:     bin,
		Cwd:         t.TempDir(),
		InitialMode: cake.RunResume,
		ResumeID:    replayTestSessionID,
	})
	if !m.hydrating {
		t.Fatal("resume model did not enter hydration state")
	}
	m.width, m.height = 80, 24
	m.layout()
	m = hydrateTestModel(t, m)

	if m.hydrating || m.replayRun != nil || m.running {
		t.Fatalf("replay did not settle: hydrating=%v replay=%v running=%v", m.hydrating, m.replayRun != nil, m.running)
	}
	if m.session.SessionID != replayTestSessionID || m.session.TaskID != "old-task" {
		t.Fatalf("session context = %+v, want replayed ids", m.session)
	}
	mode, id := m.session.RunOptions()
	if mode != cake.RunResume || id != replayTestSessionID {
		t.Fatalf("next run = %v %q, want resume %q", mode, id, replayTestSessionID)
	}

	var sawPreviousUser, sawPreviousAssistant, sawTool, sawCompletion bool
	for _, item := range m.items {
		switch {
		case item.Kind == ui.KindUser && item.Text == "previous prompt":
			sawPreviousUser = true
		case item.Kind == ui.KindAssistant && item.Text == "previous answer":
			sawPreviousAssistant = true
		case item.Kind == ui.KindTool && item.Tool != nil && item.Tool.Output == "/tmp/project":
			sawTool = true
		case item.Kind == ui.KindComplete:
			sawCompletion = true
		}
	}
	if !sawPreviousUser || !sawPreviousAssistant || !sawTool || !sawCompletion {
		t.Fatalf("replayed timeline missing expected items: %+v", m.items)
	}

	m.input.SetValue("next prompt")
	tm, _ := m.submit()
	live := tm.(Model)
	if !live.running {
		t.Fatal("prompt did not start after replay hydration")
	}
	result := collectCakeRunResult(t, live.run)
	if result.ExitCode != 0 || result.Canceled {
		t.Fatalf("continued prompt result = %+v", result)
	}
	args, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("reading captured args: %v", err)
	}
	argText := string(args)
	if !strings.Contains(argText, "--output-format\nstream-json\nreplay\n"+replayTestSessionID+"\n") {
		t.Errorf("replay invocation args = %q", argText)
	}
	if !strings.Contains(argText, "--resume\n"+replayTestSessionID+"\n--\nnext prompt\n") {
		t.Errorf("continued prompt args = %q", argText)
	}
}

func TestResumeReplayFailureWarnsAndKeepsSessionUsable(t *testing.T) {
	argsPath := filepath.Join(t.TempDir(), "args")
	t.Setenv("ARGS_FILE", argsPath)
	bin := writeFakeCake(t, `
printf '%s\n' "$@" >> "$ARGS_FILE"
if [ "$3" = "replay" ]; then
printf '%s\n' '{"type":"replay_error","session_id":"11111111-2222-3333-4444-555555555555","kind":"session_corrupt","error":"corrupt transcript","exit_code":1}'
exit 1
fi
exit 0
`)

	m := New(Config{
		CakeBin:     bin,
		Cwd:         t.TempDir(),
		InitialMode: cake.RunResume,
		ResumeID:    replayTestSessionID,
	})
	m.width, m.height = 80, 24
	m.layout()
	m = hydrateTestModel(t, m)

	if m.hydrating || m.running {
		t.Fatalf("failed replay left model unavailable: hydrating=%v running=%v", m.hydrating, m.running)
	}
	var sawWarning bool
	for _, item := range m.items {
		if item.Kind == ui.KindWarning && strings.Contains(item.Text, "session replay failed") {
			sawWarning = true
		}
	}
	if !sawWarning {
		t.Fatalf("replay failure warning missing from timeline: %+v", m.items)
	}
	mode, id := m.session.RunOptions()
	if mode != cake.RunResume || id != replayTestSessionID {
		t.Fatalf("failed replay changed next run: %v %q", mode, id)
	}

	m.input.SetValue("continue after warning")
	tm, _ := m.submit()
	live := tm.(Model)
	if !live.running {
		t.Fatal("prompt did not start after replay failure")
	}
	result := collectCakeRunResult(t, live.run)
	if result.ExitCode != 0 {
		t.Fatalf("prompt after replay failure result = %+v", result)
	}
}

func TestResumeReplayMarksDanglingToolCallDone(t *testing.T) {
	bin := writeFakeCake(t, `
printf '%s\n' '{"type":"function_call","id":"fc-1","call_id":"call-1","name":"bash","arguments":"{\"command\":\"pwd\"}"}'
`)
	m := New(Config{
		CakeBin:     bin,
		Cwd:         t.TempDir(),
		InitialMode: cake.RunResume,
		ResumeID:    replayTestSessionID,
	})
	m.width, m.height = 80, 24
	m.layout()
	m = hydrateTestModel(t, m)

	var dangling *ui.ToolBlock
	for _, item := range m.items {
		if item.Kind == ui.KindTool && item.Tool != nil {
			dangling = item.Tool
		}
	}
	if dangling == nil {
		t.Fatal("replay tool call missing from timeline")
	}
	if !dangling.Done || dangling.Output != "(no output — task ended)" {
		t.Fatalf("dangling tool = %+v, want done with task-ended marker", dangling)
	}
}
