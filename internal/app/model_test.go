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
	want := ui.RenderItems(m.theme, m.items, m.renderedWidth)
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

func TestClearResetsRenderCache(t *testing.T) {
	m := newLaidOutModel()
	m.applyEvent(cake.Message{Role: "assistant", Content: "hello"})

	tm, _ := m.execCommand(Command{Kind: CmdClear})
	got := tm.(Model)

	if len(got.items) != 0 || len(got.rendered) != 0 {
		t.Errorf("clear left items=%d rendered=%d", len(got.items), len(got.rendered))
	}
	assertCacheMatchesFullRender(t, &got, "after /clear")
}
