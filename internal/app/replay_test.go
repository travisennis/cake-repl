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

// replayStartMessage drives the same Init command batch Bubble Tea runs at
// startup and returns the replay startup message it produced.
func replayStartMessage(t *testing.T, m Model) replayStartedMsg {
	t.Helper()
	init := m.Init()
	batch, ok := init().(tea.BatchMsg)
	if !ok {
		t.Fatalf("Init message type = %T, want tea.BatchMsg", init())
	}
	for _, cmd := range batch {
		if msg := cmd(); msg != nil {
			if start, ok := msg.(replayStartedMsg); ok {
				return start
			}
		}
	}
	t.Fatal("Init did not start replay")
	return replayStartedMsg{}
}

// hydrateTestModel drives the same Init command and replay message flow that
// Bubble Tea uses at startup. It returns only after replay has finished.
func hydrateTestModel(t *testing.T, m Model) Model {
	t.Helper()
	start := replayStartMessage(t, m)

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

// Replay hydration carries the session's context measurement into the status
// line. The newest complete turn_usage record wins, and a partial record after
// it is ignored.
func TestResumeHydrationUsesLatestCompleteContextUsage(t *testing.T) {
	bin := writeFakeCake(t, `
if [ "$3" = "replay" ]; then
cat <<'EOF'
{"type":"session_meta","session_id":"11111111-2222-3333-4444-555555555555"}
{"type":"turn_usage","session_id":"11111111-2222-3333-4444-555555555555","turn":1,"usage":{"input_tokens":9000,"output_tokens":10,"total_tokens":9010},"usage_presence":"complete","context_window":1000000,"remaining_context_tokens":991000}
{"type":"turn_usage","session_id":"11111111-2222-3333-4444-555555555555","turn":2,"usage":{"input_tokens":14383,"output_tokens":195,"total_tokens":14578},"usage_presence":"complete","context_window":1000000,"remaining_context_tokens":985617}
{"type":"turn_usage","session_id":"11111111-2222-3333-4444-555555555555","turn":3,"usage":{"input_tokens":50000,"output_tokens":1,"total_tokens":50001},"usage_presence":"partial","context_window":1000000}
{"type":"task_complete","subtype":"success","is_error":false,"session_id":"11111111-2222-3333-4444-555555555555"}
EOF
fi
`)

	m := New(Config{
		CakeBin:     bin,
		Cwd:         t.TempDir(),
		InitialMode: cake.RunResume,
		ResumeID:    replayTestSessionID,
	})
	m.width, m.height = 160, 24
	m.layout()
	m = hydrateTestModel(t, m)

	if m.session.Context == nil || m.session.Context.Tokens != 14_383 || m.session.Context.Window != 1_000_000 {
		t.Fatalf("replayed context = %+v, want 14383/1000000", m.session.Context)
	}
	if line := m.statusLine(); !strings.Contains(line, "context: 14k/1M") {
		t.Errorf("status line missing replayed context: %q", line)
	}

	// The measurement belongs to the replayed session; retargeting must not
	// carry it into a different session's status line.
	tm, _ := m.resumeSession("bbbbbbbb-1111-2222-3333-444444444444")
	m = tm.(Model)
	if m.session.Context != nil {
		t.Errorf("retarget kept replayed context: %+v", m.session.Context)
	}
	if line := m.statusLine(); strings.Contains(line, "context:") {
		t.Errorf("status line still shows a context segment after retarget: %q", line)
	}
}

// A replayed checkpoint becomes one marker at the record's physical position,
// never an expansion of its retained tail, and seeds the status context with
// its own estimate until a newer complete measurement arrives.
func TestResumeHydrationRendersOneCompactionMarkerAndEstimatedContext(t *testing.T) {
	fixture, err := filepath.Abs(replayFixturePath("compaction.ndjson"))
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("REPLAY_FIXTURE", fixture)
	bin := writeFakeCake(t, `
if [ "$3" = "replay" ]; then
cat "$REPLAY_FIXTURE"
fi
`)
	m := New(Config{
		CakeBin:     bin,
		Cwd:         t.TempDir(),
		InitialMode: cake.RunResume,
		ResumeID:    replayTestSessionID,
	})
	m.width, m.height = 200, 24
	m.layout()
	m = hydrateTestModel(t, m)

	var markerIdx []int
	for i, item := range m.items {
		if item.Kind == ui.KindInfo && strings.HasPrefix(item.Text, "context compacted") {
			markerIdx = append(markerIdx, i)
		}
	}
	if len(markerIdx) != 1 {
		t.Fatalf("compaction markers = %d, want exactly one: %+v", len(markerIdx), m.items)
	}
	const wantMarker = "context compacted 118k -> 24k, 4 records retained"
	if got := m.items[markerIdx[0]].Text; got != wantMarker {
		t.Errorf("marker = %q, want %q", got, wantMarker)
	}

	// The marker sits at the record's physical position: after the completed
	// task that produced the checkpoint and before the next task's answer.
	preTool, postAnswer := -1, -1
	var completions []int
	for i, item := range m.items {
		switch {
		case item.Kind == ui.KindTool && item.Tool != nil && item.Tool.Output == "/tmp/project\n":
			preTool = i
		case item.Kind == ui.KindAssistant && item.Text == "after compaction answer":
			postAnswer = i
		case item.Kind == ui.KindComplete:
			completions = append(completions, i)
		}
	}
	if preTool < 0 || postAnswer < 0 || len(completions) != 2 {
		t.Fatalf("surrounding items missing: preTool=%d postAnswer=%d completions=%v items=%+v",
			preTool, postAnswer, completions, m.items)
	}
	preComplete, postComplete := completions[0], completions[1]
	ordered := preTool < preComplete && preComplete < markerIdx[0] &&
		markerIdx[0] < postAnswer && postAnswer < postComplete
	if !ordered {
		t.Errorf("marker at %d is not between completion %d and answer %d (tool %d, final completion %d)",
			markerIdx[0], preComplete, postAnswer, preTool, postComplete)
	}

	// The retained tail is count-only: its messages and tool call must never
	// appear as extra timeline items.
	tools := 0
	for _, item := range m.items {
		if item.Kind == ui.KindTool {
			tools++
			if item.Tool != nil && (strings.Contains(item.Tool.Output, "RETAINED-") || strings.Contains(item.Tool.Arguments, "RETAINED-")) {
				t.Errorf("retained tool content rendered: %+v", item.Tool)
			}
		}
		if strings.Contains(item.Text, "RETAINED-") {
			t.Errorf("retained message content rendered: %q", item.Text)
		}
	}
	if tools != 2 {
		t.Errorf("tool items = %d, want 2 from the surrounding transcript", tools)
	}

	context := m.session.Context
	if context == nil || context.Tokens != 24_000 || context.Window != 200_000 || !context.Estimated {
		t.Fatalf("context = %+v, want estimated 24000/200000", context)
	}
	if line := m.statusLine(); !strings.Contains(line, "context: ~24k/200k") {
		t.Errorf("status line missing the estimated context: %q", line)
	}

	// The next live measurement replaces the estimate and drops the qualifier.
	m.applyEvent(cake.TurnUsage{
		Usage:         cake.Usage{InputTokens: 6_000},
		UsagePresence: "complete",
		ContextWindow: 200_000,
	})
	line := m.statusLine()
	if !strings.Contains(line, "context: 6k/200k") || strings.Contains(line, "~6k") {
		t.Errorf("live measurement did not replace the estimate: %q", line)
	}
}

// A newer complete turn_usage replayed after the checkpoint replaces the
// checkpoint's estimated context during hydration. The marker stays at its
// physical position, but the status shows the measured figure without "~".
func TestResumeHydrationNewerReplayedMeasurementReplacesEstimate(t *testing.T) {
	fixture, err := filepath.Abs(replayFixturePath("compaction-continued.ndjson"))
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("REPLAY_FIXTURE", fixture)
	bin := writeFakeCake(t, `
if [ "$3" = "replay" ]; then
cat "$REPLAY_FIXTURE"
fi
`)
	m := New(Config{
		CakeBin:     bin,
		Cwd:         t.TempDir(),
		InitialMode: cake.RunResume,
		ResumeID:    replayTestSessionID,
	})
	m.width, m.height = 200, 24
	m.layout()
	m = hydrateTestModel(t, m)

	markers := 0
	for _, item := range m.items {
		if item.Kind == ui.KindInfo && strings.HasPrefix(item.Text, "context compacted") {
			markers++
		}
	}
	if markers != 1 {
		t.Fatalf("compaction markers = %d, want exactly one: %+v", markers, m.items)
	}

	context := m.session.Context
	if context == nil || context.Tokens != 30_000 || context.Window != 200_000 || context.Estimated {
		t.Fatalf("context = %+v, want the newer measured 30000/200000", context)
	}
	if line := m.statusLine(); !strings.Contains(line, "context: 30k/200k") || strings.Contains(line, "~30k") {
		t.Errorf("status line = %q, want the newer unqualified measurement", line)
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

// TestReplayStartupFailureShowsWarningImmediately pins the ADR 011 promise
// that a startup failure is visible and non-fatal: when the replay subprocess
// cannot even start, the warning must be on screen as soon as the startup
// message is handled, with no later key or resize to flush the view.
func TestReplayStartupFailureShowsWarningImmediately(t *testing.T) {
	m := New(Config{
		CakeBin:     filepath.Join(t.TempDir(), "missing-cake"),
		Cwd:         t.TempDir(),
		InitialMode: cake.RunResume,
		ResumeID:    replayTestSessionID,
	})
	m.width, m.height = 80, 24
	m.layout()
	m.syncViewport()

	start := replayStartMessage(t, m)
	if start.err == nil {
		t.Fatalf("missing cake binary started replay: %+v", start)
	}

	tm, cmd := m.Update(start)
	m = tm.(Model)
	if cmd != nil {
		t.Fatal("failed replay startup returned an unexpected command")
	}
	if m.hydrating || m.replayPending {
		t.Fatalf("failed replay left hydration active: hydrating=%v pending=%v", m.hydrating, m.replayPending)
	}
	if view := m.View(); !strings.Contains(view, "session replay unavailable") {
		t.Fatalf("startup warning not visible without another event:\n%s", view)
	}
	if mode, id := m.session.RunOptions(); mode != cake.RunResume || id != replayTestSessionID {
		t.Fatalf("failed replay changed the startup pin: %v %q", mode, id)
	}
}

// TestReplayStartupFailureHonorsPendingExit covers /exit while replay is
// starting: the failed startup still quits, and the warning is synced into
// the frame the quit renders.
func TestReplayStartupFailureHonorsPendingExit(t *testing.T) {
	m := New(Config{
		CakeBin:     filepath.Join(t.TempDir(), "missing-cake"),
		Cwd:         t.TempDir(),
		InitialMode: cake.RunResume,
		ResumeID:    replayTestSessionID,
	})
	m.width, m.height = 80, 24
	m.layout()
	m.syncViewport()
	start := replayStartMessage(t, m)

	tm, cmd := m.execCommand(Command{Kind: CmdExit})
	m = tm.(Model)
	if !m.exitAfter || cmd != nil {
		t.Fatalf("exit while hydrating: exitAfter=%v cmd set=%v", m.exitAfter, cmd != nil)
	}

	tm, cmd = m.Update(start)
	m = tm.(Model)
	if view := m.View(); !strings.Contains(view, "session replay unavailable") {
		t.Fatalf("startup warning missing from the final frame:\n%s", view)
	}
	if cmd == nil {
		t.Fatal("failed replay startup did not return the pending quit")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatalf("cmd() = %T, want tea.QuitMsg", cmd())
	}
}

// TestReplayStartupFailureAfterNewSessionStaysBehindTheBoundary covers Ctrl+N
// pressed while replay is starting: the failed startup message still releases
// the pending state, but neither its warning nor its session context crosses
// the new-session boundary.
func TestReplayStartupFailureAfterNewSessionStaysBehindTheBoundary(t *testing.T) {
	m := New(Config{
		CakeBin:     filepath.Join(t.TempDir(), "missing-cake"),
		Cwd:         t.TempDir(),
		InitialMode: cake.RunResume,
		ResumeID:    replayTestSessionID,
	})
	m.width, m.height = 80, 24
	m.layout()
	start := replayStartMessage(t, m)

	tm, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlN})
	m = tm.(Model)
	if cmd != nil {
		t.Fatalf("ctrl+n returned a command: %v", cmd)
	}
	if m.hydrating || !m.replayPending {
		t.Fatalf("ctrl+n state: hydrating=%v pending=%v", m.hydrating, m.replayPending)
	}

	tm, cmd = m.Update(start)
	m = tm.(Model)
	if cmd != nil {
		t.Fatal("failed startup after ctrl+n returned a command")
	}
	if m.replayPending {
		t.Fatal("failed startup left the replay pending after ctrl+n")
	}
	if view := m.View(); strings.Contains(view, "session replay unavailable") {
		t.Fatalf("startup warning crossed the new-session boundary:\n%s", view)
	}
	if mode, id := m.session.RunOptions(); mode != cake.RunFresh || id != "" {
		t.Fatalf("failed startup re-pinned the left session: %v %q", mode, id)
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

// replayFixturePath resolves a fixture shared with the cake package tests.
func replayFixturePath(name string) string {
	return filepath.Join("..", "cake", "testdata", "fixtures", name)
}

func TestResumeHydratesModelFromReplayFixture(t *testing.T) {
	fixture, err := filepath.Abs(replayFixturePath("replay-model.ndjson"))
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("REPLAY_FIXTURE", fixture)
	bin := writeFakeCake(t, `
if [ "$3" = "replay" ]; then
cat "$REPLAY_FIXTURE"
fi
`)
	m := New(Config{
		CakeBin:     bin,
		Cwd:         t.TempDir(),
		Model:       "cli-model",
		InitialMode: cake.RunResume,
		ResumeID:    replayTestSessionID,
	})
	m.width, m.height = 120, 24
	m.layout()
	m = hydrateTestModel(t, m)

	// session_meta carries both identities; the [[models]] entry name wins.
	if got := m.displayModel(); got != "zen" {
		t.Fatalf("display model = %q, want zen from session_meta.model_config", got)
	}
	line := m.statusLine()
	if !strings.Contains(line, "model:") || !strings.Contains(line, "zen") {
		t.Errorf("status line missing replayed model: %q", line)
	}
	if strings.Contains(line, "cli-model") {
		t.Errorf("status line still shows the CLI model: %q", line)
	}
}

func TestResumeWithoutModelIdentityFallsBackToConfig(t *testing.T) {
	bin := writeFakeCake(t, `
printf '%s\n' '{"type":"session_meta","session_id":"11111111-2222-3333-4444-555555555555","working_directory":"/tmp/project"}'
printf '%s\n' '{"type":"task_start","session_id":"11111111-2222-3333-4444-555555555555","task_id":"old-task"}'
printf '%s\n' '{"type":"task_complete","subtype":"success","is_error":false,"session_id":"11111111-2222-3333-4444-555555555555","task_id":"old-task"}'
`)
	m := New(Config{
		CakeBin:     bin,
		Cwd:         t.TempDir(),
		Model:       "cli-model",
		InitialMode: cake.RunResume,
		ResumeID:    replayTestSessionID,
	})
	m.width, m.height = 120, 24
	m.layout()
	m = hydrateTestModel(t, m)

	if got := m.displayModel(); got != "cli-model" {
		t.Fatalf("display model = %q, want CLI fallback", got)
	}
	if line := m.statusLine(); !strings.Contains(line, "model:") || !strings.Contains(line, "cli-model") {
		t.Errorf("status line missing CLI fallback model: %q", line)
	}
}

func TestResumeWithoutAnyModelOmitsStatusModel(t *testing.T) {
	bin := writeFakeCake(t, `
printf '%s\n' '{"type":"session_meta","session_id":"11111111-2222-3333-4444-555555555555","working_directory":"/tmp/project"}'
`)
	m := New(Config{
		CakeBin:     bin,
		Cwd:         t.TempDir(),
		InitialMode: cake.RunResume,
		ResumeID:    replayTestSessionID,
	})
	m.width, m.height = 120, 24
	m.layout()
	m = hydrateTestModel(t, m)

	if got := m.displayModel(); got != "" {
		t.Fatalf("display model = %q, want empty", got)
	}
	if line := m.statusLine(); strings.Contains(line, "model:") {
		t.Errorf("status line rendered a model field with no model anywhere: %q", line)
	}
}
