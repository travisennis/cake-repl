package app

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/travisennis/cake-repl/internal/cake"
	"github.com/travisennis/cake-repl/internal/ui"
)

// newLaidOutModel returns a model after its first layout, as if the program
// received the initial WindowSizeMsg.
func newLaidOutModel() Model {
	m := New(Config{})
	m.width, m.height = 80, 24
	m.layout()
	return m
}

// titleSetBy returns the terminal title cmd would set. Bubble Tea's title
// message type is unexported, so the value is read through reflection.
func titleSetBy(cmd tea.Cmd) (string, bool) {
	if cmd == nil {
		return "", false
	}
	rv := reflect.ValueOf(cmd())
	if rv.IsValid() && rv.Kind() == reflect.String {
		return rv.String(), true
	}
	return "", false
}

// requireTitle fails unless cmd sets the terminal title to want.
func requireTitle(t *testing.T, cmd tea.Cmd, want string) {
	t.Helper()
	got, ok := titleSetBy(cmd)
	if !ok {
		t.Fatalf("command %v does not set a terminal title", cmd)
	}
	if got != want {
		t.Errorf("terminal title = %q, want %q", got, want)
	}
}

// requireBatchTitle fails unless cmd is a batch whose last command sets the
// terminal title to want. Only that command runs: the other members of a run
// start's batch are the spinner tick and the run wait, and executing the wait
// would consume the run's events out from under the test.
func requireBatchTitle(t *testing.T, cmd tea.Cmd, want string) {
	t.Helper()
	if cmd == nil {
		t.Fatal("no command returned")
	}
	cmds, ok := cmd().(tea.BatchMsg)
	if !ok {
		t.Fatalf("cmd() = %T, want tea.BatchMsg", cmd())
	}
	if len(cmds) == 0 {
		t.Fatal("batch is empty")
	}
	requireTitle(t, cmds[len(cmds)-1], want)
}

func TestInitSetsWindowTitleToWorkingDirectory(t *testing.T) {
	m := New(Config{Cwd: "/tmp/project\x1b]2;injected\a"})
	msg := m.Init()()
	cmds, ok := msg.(tea.BatchMsg)
	if !ok {
		t.Fatalf("Init message type = %T, want tea.BatchMsg", msg)
	}

	blinkType := reflect.TypeOf(textarea.Blink())
	blinkFound := false
	for _, cmd := range cmds {
		if reflect.TypeOf(cmd()) == blinkType {
			blinkFound = true
		}
	}
	requireBatchTitle(t, tea.Batch(cmds...), "cake-repl: /tmp/project")
	if !blinkFound {
		t.Error("Init did not preserve textarea blinking")
	}
}

func TestTitleCmdTracksWorkingState(t *testing.T) {
	// The title is the only status signal that survives an unfocused or
	// minimized window, so the working marker must follow m.running exactly.
	m := New(Config{Cwd: "/tmp/project\x1b]2;injected\a"})
	requireTitle(t, m.titleCmd(), "cake-repl: /tmp/project")

	m.running = true
	requireTitle(t, m.titleCmd(), "[working] cake-repl: /tmp/project")
}

func TestIdleTitleSequenceRestoresIdleTitle(t *testing.T) {
	// main writes this for the exits the model never sees: an interrupt, a
	// termination signal, or a panic that returns no model.
	want := "\x1b]2;cake-repl: /tmp/project\x07"
	if got := IdleTitleSequence("/tmp/project\x1b]2;injected\a"); got != want {
		t.Errorf("idle title sequence = %q, want %q", got, want)
	}
}

func TestFinishRunResetsTitleToIdle(t *testing.T) {
	m := newLaidOutModel()
	m.cfg.Cwd = "/tmp/project"
	m.running = true

	tm, cmd := m.finishRun(cake.Result{ExitCode: 0})
	got := tm.(Model)
	if got.running {
		t.Fatal("finishRun left the model running")
	}
	requireTitle(t, cmd, "cake-repl: /tmp/project")
}

// renderItemsFull is the reference implementation for timeline join: each item
// is rendered independently via RenderItem and joined with a double newline.
func renderItemsFull(th ui.Theme, items []ui.Item, width int, outputLimit int, toolOutputMode ui.ToolOutputMode) string {
	if outputLimit <= 0 {
		outputLimit = ui.DefaultOutputLimit
	}
	parts := make([]string, 0, len(items))
	for _, it := range items {
		parts = append(parts, ui.RenderItem(th, it, width, outputLimit, toolOutputMode))
	}
	return strings.Join(parts, "\n\n")
}

// assertCacheMatchesFullRender checks the incremental render cache against
// the documented RenderItem + double-newline join. The viewport payload is
// the same join, computed lazily at sync time.
func assertCacheMatchesFullRender(t *testing.T, m *Model, step string) {
	t.Helper()
	want := renderItemsFull(m.theme, m.items, m.renderedWidth, m.cfg.OutputLimit, m.toolOutputMode)
	got := strings.Join(m.rendered, "\n\n")
	if got != want {
		t.Fatalf("%s: cached render diverged from full render\ngot:\n%s\nwant:\n%s", step, got, want)
	}
}

func TestTimelineCacheMatchesFullRender(t *testing.T) {
	m := newLaidOutModel()
	assertCacheMatchesFullRender(t, &m, "after initial layout")

	m.applyEvent(cake.TaskStart{SessionID: "11111111-2222-3333-4444-555555555555", TaskID: "t-1"})
	assertCacheMatchesFullRender(t, &m, "after task_start")

	m.applyEvent(cake.Message{Role: "assistant", Content: "héllo 日本語のテキスト"})
	assertCacheMatchesFullRender(t, &m, "after assistant message")

	m.applyEvent(cake.FunctionCall{ID: "fc-1", CallID: "c-1", Name: "bash", Arguments: `{"command":"ls -la"}`})
	assertCacheMatchesFullRender(t, &m, "after function_call")

	m.applyEvent(cake.FunctionCallOutput{CallID: "c-1", Output: "file.txt\nother.txt"})
	assertCacheMatchesFullRender(t, &m, "after in-place tool output")

	m.applyEvent(cake.FunctionCallOutput{CallID: "c-unknown", Output: "late"})
	assertCacheMatchesFullRender(t, &m, "after orphan tool output")

	m.width = 60
	m.layout()
	if m.renderedWidth != 60 {
		t.Errorf("renderedWidth = %d after resize, want 60", m.renderedWidth)
	}
	assertCacheMatchesFullRender(t, &m, "after width change")
}

func TestHeightOnlyLayoutDoesNotRerender(t *testing.T) {
	m := newLaidOutModel()
	m.applyEvent(cake.Message{Role: "assistant", Content: "hello"})

	m.rendered[0] = "sentinel"
	m.height = 40
	m.layout()
	if m.rendered[0] != "sentinel" {
		t.Error("height-only resize should not re-render items")
	}

	m.width = 79
	m.layout()
	if m.rendered[0] == "sentinel" {
		t.Error("width change should re-render items")
	}
}

func TestLayoutAccountsForPromptComposerChrome(t *testing.T) {
	m := newLaidOutModel()
	wantMinHeight := 24 - minInputHeight - composerVerticalChrome - statusHeight
	if m.timeline.Height != wantMinHeight {
		t.Fatalf("minimum-input timeline height = %d, want %d", m.timeline.Height, wantMinHeight)
	}
	inputLine := strings.Split(m.input.View(), "\n")[0]
	if got := lipgloss.Width(inputLine); got != 80-composerHorizontalChrome {
		t.Fatalf("rendered input width = %d, want %d", got, 80-composerHorizontalChrome)
	}

	m.input.SetValue(strings.Repeat("line\n", maxInputHeight+2))
	m.layout()
	wantMaxHeight := 24 - maxInputHeight - composerVerticalChrome - statusHeight
	if m.timeline.Height != wantMaxHeight {
		t.Fatalf("maximum-input timeline height = %d, want %d", m.timeline.Height, wantMaxHeight)
	}
	if got := len(strings.Split(m.View(), "\n")); got != m.height {
		t.Errorf("view height = %d lines, want terminal height %d", got, m.height)
	}
}

func TestAppendItemExtendsRenderCacheWithoutRebuild(t *testing.T) {
	m := newLaidOutModel()
	before := len(m.rendered)
	m.rendered[0] = "sentinel"
	m.appendItem(ui.Item{Kind: ui.KindAssistant, Text: "hello"})

	if m.rendered[0] != "sentinel" {
		t.Fatal("append rebuilt existing cache entries")
	}
	if len(m.rendered) != before+1 {
		t.Fatalf("render cache length = %d, want %d", len(m.rendered), before+1)
	}
	if !m.timelineDirty {
		t.Error("append did not mark the viewport payload dirty")
	}
}

func TestToolOutputRerenderPreservesBottomPinning(t *testing.T) {
	m := New(Config{})
	m.width, m.height = 40, 8
	m.layout()

	for range 8 {
		m.applyEvent(cake.Message{Role: "assistant", Content: "line one\nline two\nline three"})
	}
	m.applyEvent(cake.FunctionCall{CallID: "c-1", Name: "bash", Arguments: `{"command":"printf hi"}`})
	m.timeline.GotoBottom()
	if !m.timeline.AtBottom() {
		t.Fatal("test setup did not reach bottom")
	}

	// Production drives applyEvent through Update, which syncs the viewport
	// once per batch; mirror that here so the pinning logic runs.
	m.applyEvent(cake.FunctionCallOutput{CallID: "c-1", Output: "hi"})
	m.syncViewport()

	if !m.timeline.AtBottom() {
		t.Fatal("tool output rerender should preserve bottom pinning")
	}
	if !strings.Contains(strings.Join(m.rendered, "\n\n"), "hi") {
		t.Fatal("tool output rerender did not update timeline content")
	}
	assertCacheMatchesFullRender(t, &m, "after pinned tool output rerender")
}

func TestFinishRunRerendersOrphanedToolCalls(t *testing.T) {
	m := newLaidOutModel()
	m.applyEvent(cake.FunctionCall{CallID: "c-1", Name: "bash", Arguments: `{"command":"sleep 5"}`})

	tm, _ := m.finishRun(cake.Result{ExitCode: 0})
	got := tm.(Model)

	if len(got.pendingCalls) != 0 {
		t.Errorf("pendingCalls not drained: %v", got.pendingCalls)
	}
	if !strings.Contains(strings.Join(got.rendered, "\n\n"), "no output — task ended") {
		t.Error("orphaned tool call not re-rendered with its terminal state")
	}
	assertCacheMatchesFullRender(t, &got, "after finishRun")
}

func TestCancelRunningNoopWhenNotRunning(t *testing.T) {
	var m Model
	m.CancelRunning() // must not panic or hang
}

func TestCancelRunningCancelsActiveRun(t *testing.T) {
	bin := writeFakeCake(t, `
cat <<'EOF'
{"type":"task_start","session_id":"11111111-2222-3333-4444-555555555555","task_id":"t-1","timestamp":"2026-06-09T12:00:00Z"}
EOF
sleep 30
exit 0
`)
	run, err := cake.Start(cake.Options{Bin: bin, Prompt: "hi"})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Consume the one event so the goroutine lands in stdout reads.
	<-run.Events

	m := Model{run: run, running: true}
	m.CancelRunning()

	// The result should indicate cancellation.
	res := <-run.Result
	if !res.Canceled {
		t.Errorf("expected canceled run, got exit_code=%d canceled=%v err=%v",
			res.ExitCode, res.Canceled, res.Err)
	}
}

func TestClearResetsRenderCache(t *testing.T) {
	m := newLaidOutModel()
	m.applyEvent(cake.Message{Role: "assistant", Content: "hello"})
	m.toolOutputMode = ui.ToolOutputHidden

	tm, _ := m.execCommand(Command{Kind: CmdClear})
	got := tm.(Model)
	// Production syncs once per Update after the command handler runs; mirror
	// that here so the cleared payload is pushed to the viewport.
	got.syncViewport()

	if len(got.items) != 0 || len(got.rendered) != 0 {
		t.Errorf("clear left items=%d rendered=%d", len(got.items), len(got.rendered))
	}
	if got.timelineDirty {
		t.Errorf("clear left the viewport payload dirty")
	}
	if got.toolOutputMode != ui.ToolOutputHidden {
		t.Errorf("clear reset tool output mode: got %v, want hidden", got.toolOutputMode)
	}
	assertCacheMatchesFullRender(t, &got, "after /clear")
}

func TestClearReleasesRenderedBackingArray(t *testing.T) {
	m := newLaidOutModel()
	m.applyEvent(cake.Message{Role: "assistant", Content: strings.Repeat("hello ", 100)})
	if cap(m.rendered) == 0 {
		t.Fatal("setup: render cache has no backing array to release")
	}

	tm, _ := m.execCommand(Command{Kind: CmdClear})
	got := tm.(Model)
	got.syncViewport()

	if got.rendered != nil {
		t.Errorf("clear kept the render cache backing array (len=%d cap=%d); old rendered strings stay pinned", len(got.rendered), cap(got.rendered))
	}
}

func TestNewSessionReleasesRenderedBackingArray(t *testing.T) {
	m := newLaidOutModel()
	m.applyEvent(cake.Message{Role: "assistant", Content: strings.Repeat("hello ", 100)})

	tm, _ := m.startNewSession()
	got := tm.(Model)
	got.syncViewport()

	if len(got.rendered) != 1 || cap(got.rendered) != 1 {
		t.Errorf("new session cache len=%d cap=%d, want 1/1 (fresh backing array, old strings released)", len(got.rendered), cap(got.rendered))
	}
	if len(got.items) != 1 || got.items[0].Kind != ui.KindInfo {
		t.Errorf("new session items = %+v, want the info banner only", got.items)
	}
}

func TestToolOutputRetentionCapAppliesAtIngest(t *testing.T) {
	m := newLaidOutModel()
	m.applyEvent(cake.FunctionCall{CallID: "c-1", Name: "bash", Arguments: `{"command":"cat big.bin"}`})
	m.applyEvent(cake.FunctionCallOutput{CallID: "c-1", Output: strings.Repeat("x", 2<<20)})

	tool := m.items[len(m.items)-1].Tool
	if len(tool.Output) >= 2<<20 {
		t.Fatalf("retained %d bytes of oversized output; want the ingest cap applied", len(tool.Output))
	}
	if len(tool.Output) >= maxRetainedToolOutputBytes+128 {
		t.Errorf("retained %d bytes, want <= ceiling %d plus marker slack", len(tool.Output), maxRetainedToolOutputBytes)
	}
	if !strings.Contains(tool.Output, "truncated") {
		t.Errorf("oversized output kept no truncation marker: %q", tool.Output)
	}

	// Normal-sized output passes through byte-identical (trailing newlines
	// included), so Ctrl+O full mode is unchanged for realistic results. The
	// orphan branch (unknown call id) applies the same cap.
	m.applyEvent(cake.FunctionCallOutput{CallID: "c-unknown", Output: "hello"})
	if last := m.items[len(m.items)-1]; last.Tool.Output != "hello" {
		t.Errorf("orphan normal output = %q, want verbatim 'hello'", last.Tool.Output)
	}
	m.applyEvent(cake.FunctionCallOutput{CallID: "c-unknown", Output: "hello\n"})
	if last := m.items[len(m.items)-1]; last.Tool.Output != "hello\n" {
		t.Errorf("orphan newline-terminated output = %q, want verbatim 'hello\\n'", last.Tool.Output)
	}
	m.applyEvent(cake.FunctionCallOutput{CallID: "c-unknown", Output: strings.Repeat("y", 2<<20)})
	if last := m.items[len(m.items)-1]; len(last.Tool.Output) >= 2<<20 {
		t.Errorf("orphan oversized output retained %d bytes; want the ingest cap applied", len(last.Tool.Output))
	}
}

func TestMaxTimelineItemsTrimReleasesTrimmedPayloads(t *testing.T) {
	m := New(Config{MaxTimelineItems: 3})
	m.width, m.height = 80, 24
	m.layout()
	for i := range 5 {
		m.applyEvent(cake.FunctionCall{CallID: fmt.Sprintf("c-%d", i), Name: "bash", Arguments: `{}`})
		m.applyEvent(cake.FunctionCallOutput{CallID: fmt.Sprintf("c-%d", i), Output: strings.Repeat("x", (i+1)<<10)})
	}

	if len(m.items) != 3 || len(m.rendered) != 3 {
		t.Fatalf("timeline after trim: items=%d rendered=%d, want 3/3", len(m.items), len(m.rendered))
	}
	// trimFront copies survivors into a fresh exact-size slice; extra capacity
	// would mean the old backing array (and the trimmed items' payloads) is
	// still reachable.
	if cap(m.items) != len(m.items) {
		t.Errorf("items cap=%d > len=%d; trimmed item payloads stay pinned", cap(m.items), len(m.items))
	}
	assertCacheMatchesFullRender(t, &m, "after capped trim")
}
