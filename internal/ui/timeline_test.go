package ui

import (
	"strings"
	"testing"
)

func TestRenderItemTool(t *testing.T) {
	th := DefaultTheme()

	tests := []struct {
		name   string
		item   Item
		width  int
		checks func(t *testing.T, got string)
	}{
		{
			name:  "nil tool returns empty",
			item:  Item{Kind: KindTool, Tool: nil},
			width: 80,
			checks: func(t *testing.T, got string) {
				if got != "" {
					t.Errorf("expected empty string, got %q", got)
				}
			},
		},
		{
			name: "single-line bash tool header and output",
			item: Item{
				Kind: KindTool,
				Tool: &ToolBlock{
					Name:      "bash",
					Arguments: `{"command":"ls -la"}`,
					Output:    "file1.txt\nfile2.txt\n",
					Done:      true,
				},
			},
			width: 200,
			checks: func(t *testing.T, got string) {
				if !strings.Contains(got, "⚙ bash") {
					t.Errorf("missing bash header: %q", got)
				}
				if !strings.Contains(got, "$ ls -la") {
					t.Errorf("missing command summary: %q", got)
				}
				if !strings.Contains(got, "file1.txt") {
					t.Errorf("missing tool output: %q", got)
				}
				if strings.Contains(got, "… running") {
					t.Errorf("unexpected running indicator: %q", got)
				}
				if strings.Contains(got, "(no output)") {
					t.Errorf("unexpected no-output indicator: %q", got)
				}
			},
		},
		{
			name: "multi-line edit tool shows edit previews",
			item: Item{
				Kind: KindTool,
				Tool: &ToolBlock{
					Name:      "edit",
					Arguments: `{"path":"main.go","edits":[{"old_text":"foo","new_text":"bar"},{"old_text":"hello","new_text":"world"}]}`,
					Output:    "applied edits\n",
					Done:      true,
				},
			},
			width: 200,
			checks: func(t *testing.T, got string) {
				if !strings.Contains(got, "⚙ edit") {
					t.Errorf("missing edit header: %q", got)
				}
				if !strings.Contains(got, "main.go (2 edits)") {
					t.Errorf("missing path/count: %q", got)
				}
				if !strings.Contains(got, "foo → bar") {
					t.Errorf("missing first edit preview: %q", got)
				}
				if !strings.Contains(got, "hello → world") {
					t.Errorf("missing second edit preview: %q", got)
				}
				if !strings.Contains(got, "applied edits") {
					t.Errorf("missing tool output: %q", got)
				}
			},
		},
		{
			name: "edit preview cap limits to maxEditPreviews",
			item: Item{
				Kind: KindTool,
				Tool: &ToolBlock{
					Name: "edit",
					Arguments: `{"path":"a.go","edits":[` +
						`{"old_text":"1","new_text":"a"},{"old_text":"2","new_text":"b"},` +
						`{"old_text":"3","new_text":"c"},{"old_text":"4","new_text":"d"},` +
						`{"old_text":"5","new_text":"e"}]}`,
					Output: "done\n",
					Done:   true,
				},
			},
			width: 200,
			checks: func(t *testing.T, got string) {
				if !strings.Contains(got, "… 2 more") {
					t.Errorf("expected preview cap marker, got: %q", got)
				}
				if !strings.Contains(got, "1 → a") {
					t.Errorf("missing first edit preview: %q", got)
				}
				if !strings.Contains(got, "3 → c") {
					t.Errorf("missing third edit preview: %q", got)
				}
				if strings.Contains(got, "4 → d") {
					t.Errorf("fourth edit should not appear: %q", got)
				}
				if strings.Contains(got, "5 → e") {
					t.Errorf("fifth edit should not appear: %q", got)
				}
			},
		},
		{
			name: "running tool shows running indicator",
			item: Item{
				Kind: KindTool,
				Tool: &ToolBlock{
					Name:      "bash",
					Arguments: `{"command":"sleep 1"}`,
					Output:    "",
					Done:      false,
				},
			},
			width: 200,
			checks: func(t *testing.T, got string) {
				if !strings.Contains(got, "… running") {
					t.Errorf("expected running indicator, got: %q", got)
				}
				if strings.Contains(got, "(no output)") {
					t.Errorf("unexpected no-output indicator for running tool: %q", got)
				}
			},
		},
		{
			name: "done tool with empty output shows no-output indicator",
			item: Item{
				Kind: KindTool,
				Tool: &ToolBlock{
					Name:      "bash",
					Arguments: `{"command":"echo done"}`,
					Output:    "",
					Done:      true,
				},
			},
			width: 200,
			checks: func(t *testing.T, got string) {
				if !strings.Contains(got, "(no output)") {
					t.Errorf("expected no-output indicator, got: %q", got)
				}
				if strings.Contains(got, "… running") {
					t.Errorf("unexpected running indicator for done tool: %q", got)
				}
			},
		},
		{
			// Output composed entirely of whitespace should also trigger
			// the no-output branch.
			name: "done tool with whitespace-only output shows no-output indicator",
			item: Item{
				Kind: KindTool,
				Tool: &ToolBlock{
					Name:      "bash",
					Arguments: `{"command":"echo whitespace"}`,
					Output:    "  \n  \n  ",
					Done:      true,
				},
			},
			width: 200,
			checks: func(t *testing.T, got string) {
				if !strings.Contains(got, "(no output)") {
					t.Errorf("expected no-output indicator for whitespace-only output, got: %q", got)
				}
			},
		},
		{
			name: "output truncation shows truncated marker",
			item: Item{
				Kind: KindTool,
				Tool: &ToolBlock{
					Name:      "bash",
					Arguments: `{"command":"make"}`,
					Output:    strings.Repeat("x", DefaultOutputLimit+500),
					Done:      true,
				},
			},
			width: 200,
			checks: func(t *testing.T, got string) {
				if !strings.Contains(got, "truncated") {
					t.Errorf("expected truncation marker, got: %q", got)
				}
				if !strings.Contains(got, "2500 bytes") {
					t.Errorf("expected original byte count in truncation, got: %q", got)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RenderItem(th, tt.item, tt.width)
			tt.checks(t, got)
		})
	}
}

func TestRenderItem_NonToolKinds(t *testing.T) {
	th := DefaultTheme()

	tests := []struct {
		name    string
		kind    Kind
		text    string
		wantSub string
	}{
		{"user", KindUser, "hello", "❯"},
		{"assistant", KindAssistant, "world", "world"},
		{"reasoning", KindReasoning, "thinking", "·"},
		{"hook", KindHook, "hook msg", "⚑"},
		{"task_start", KindTaskStart, "task info", "—"},
		{"complete", KindComplete, "done", "✓"},
		{"error", KindError, "boom", "✗"},
		{"warning", KindWarning, "caution", "!"},
		{"info", KindInfo, "info text", "info text"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RenderItem(th, Item{Kind: tt.kind, Text: tt.text}, 80)
			if got == "" {
				t.Errorf("RenderItem(%v) returned empty", tt.kind)
			}
			if !strings.Contains(got, tt.wantSub) {
				t.Errorf("RenderItem(%v) = %q, want it to contain %q", tt.kind, got, tt.wantSub)
			}
		})
	}
}

func TestRenderItems_JoinsWithDoubleNewline(t *testing.T) {
	th := DefaultTheme()
	items := []Item{
		{Kind: KindInfo, Text: "first"},
		{Kind: KindInfo, Text: "second"},
	}
	got := RenderItems(th, items, 80)
	if !strings.Contains(got, "\n\n") {
		t.Errorf("expected items joined by double newline, got %q", got)
	}
}

func TestRenderItem_NarrowWidth(t *testing.T) {
	th := DefaultTheme()
	item := Item{Kind: KindInfo, Text: "hello"}
	got := RenderItem(th, item, 3) // width < 8, should clamp to 8
	want := th.Info.Width(8).Render("hello")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRenderItem_DefaultBranch(t *testing.T) {
	th := DefaultTheme()
	// Use a Kind value outside the known range to hit the default branch,
	// which renders with the Debug style.
	got := RenderItem(th, Item{Kind: Kind(99), Text: "fallback"}, 80)
	if got == "" {
		t.Errorf("default branch returned empty")
	}
	if !strings.Contains(got, "fallback") {
		t.Errorf("default branch should contain text, got %q", got)
	}
}
