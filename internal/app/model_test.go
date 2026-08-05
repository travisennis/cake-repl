package app

import (
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

func TestInitSetsWindowTitleToWorkingDirectory(t *testing.T) {
	m := New(Config{Cwd: "/tmp/project\x1b]2;injected\a"})
	msg := m.Init()()
	cmds, ok := msg.(tea.BatchMsg)
	if !ok {
		t.Fatalf("Init message type = %T, want tea.BatchMsg", msg)
	}

	var title string
	var blinkFound bool
	blinkType := reflect.TypeOf(textarea.Blink())
	for _, cmd := range cmds {
		cmdMsg := cmd()
		if reflect.TypeOf(cmdMsg) == blinkType {
			blinkFound = true
		}
		value := reflect.ValueOf(cmdMsg)
		if value.IsValid() && value.Kind() == reflect.String {
			title = value.String()
		}
	}
	if want := "cake-repl: /tmp/project"; title != want {
		t.Errorf("window title = %q, want %q", title, want)
	}
	if !blinkFound {
		t.Error("Init did not preserve textarea blinking")
	}
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
// the documented RenderItem + double-newline join.
func assertCacheMatchesFullRender(t *testing.T, m *Model, step string) {
	t.Helper()
	want := renderItemsFull(m.theme, m.items, m.renderedWidth, m.cfg.OutputLimit, m.toolOutputMode)
	got := strings.Join(m.rendered, "\n\n")
	if got != want {
		t.Fatalf("%s: cached render diverged from full render\ngot:\n%s\nwant:\n%s", step, got, want)
	}
	if m.timelineContent != want {
		t.Fatalf("%s: timeline content diverged from full render\ngot:\n%s\nwant:\n%s", step, m.timelineContent, want)
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

func TestAppendItemExtendsTimelineContentWithoutFullJoin(t *testing.T) {
	m := newLaidOutModel()
	before := m.timelineContent
	rendered := ui.RenderItem(m.theme, ui.Item{Kind: ui.KindAssistant, Text: "hello"}, m.renderedWidth, m.cfg.OutputLimit, m.toolOutputMode)

	m.rendered[0] = "sentinel"
	m.appendItem(ui.Item{Kind: ui.KindAssistant, Text: "hello"})

	want := before + "\n\n" + rendered
	if m.timelineContent != want {
		t.Fatalf("append rebuilt content from rendered cache\ngot:\n%s\nwant:\n%s", m.timelineContent, want)
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

	m.applyEvent(cake.FunctionCallOutput{CallID: "c-1", Output: "hi"})

	if !m.timeline.AtBottom() {
		t.Fatal("tool output rerender should preserve bottom pinning")
	}
	if !strings.Contains(m.timelineContent, "hi") {
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

	if len(got.items) != 0 || len(got.rendered) != 0 {
		t.Errorf("clear left items=%d rendered=%d", len(got.items), len(got.rendered))
	}
	if got.timelineContent != "" {
		t.Errorf("clear left timelineContent=%q", got.timelineContent)
	}
	if got.toolOutputMode != ui.ToolOutputHidden {
		t.Errorf("clear reset tool output mode: got %v, want hidden", got.toolOutputMode)
	}
	assertCacheMatchesFullRender(t, &got, "after /clear")
}
