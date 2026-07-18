package app

import (
	"strings"
	"testing"

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

// assertCacheMatchesFullRender checks the incremental render cache against
// RenderItems, the reference implementation.
func assertCacheMatchesFullRender(t *testing.T, m *Model, step string) {
	t.Helper()
	want := ui.RenderItems(m.theme, m.items, m.renderedWidth, m.cfg.OutputLimit, m.toolOutputMode)
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
