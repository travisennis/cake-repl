package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
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
		{
			name: "long tool argument at narrow width does not overflow",
			item: Item{
				Kind: KindTool,
				Tool: &ToolBlock{
					Name:      "read",
					Arguments: `{"path":"/a/very/long/path/that/exceeds/the/available/width/in/the/header/because/the/prefix/needs/room/toooo.txt"}`,
					Output:    "some content\n",
					Done:      true,
				},
			},
			width: 30,
			checks: func(t *testing.T, got string) {
				// Header line must fit within width cells. lipgloss.Width
				// measures the rendered string's visual width after stripping
				// ANSI codes.
				for _, line := range strings.Split(got, "\n") {
					if lipgloss.Width(line) > 30 {
						t.Errorf("line %q has width %d, exceeds 30", line, lipgloss.Width(line))
					}
				}
				if !strings.Contains(got, "⚙ read") {
					t.Errorf("missing read header: %q", got)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RenderItem(th, tt.item, tt.width, DefaultOutputLimit)
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
			got := RenderItem(th, Item{Kind: tt.kind, Text: tt.text}, 80, DefaultOutputLimit)
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
	got := RenderItems(th, items, 80, DefaultOutputLimit)
	if !strings.Contains(got, "\n\n") {
		t.Errorf("expected items joined by double newline, got %q", got)
	}
}

func TestRenderItem_NarrowWidth(t *testing.T) {
	th := DefaultTheme()
	item := Item{Kind: KindInfo, Text: "hello"}
	got := RenderItem(th, item, 3, DefaultOutputLimit) // width < 8, should clamp to 8
	want := th.Info.Width(8).Render("hello")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRenderItem_DefaultBranch(t *testing.T) {
	th := DefaultTheme()
	// Use a Kind value outside the known range to hit the default branch,
	// which renders with the Debug style.
	got := RenderItem(th, Item{Kind: Kind(99), Text: "fallback"}, 80, DefaultOutputLimit)
	if got == "" {
		t.Errorf("default branch returned empty")
	}
	if !strings.Contains(got, "fallback") {
		t.Errorf("default branch should contain text, got %q", got)
	}
}

// reconstructHeader strips the styled prefix from the first header line and
// the continuation indent from subsequent wrapped header lines, returning the
// joined argument content. Reconstruction stops at the first line that does
// not start with the continuation indent (e.g. the tool output block).
func reconstructHeader(rendered, prefix string, indent int) string {
	pad := strings.Repeat(" ", indent)
	var b strings.Builder
	for i, line := range strings.Split(rendered, "\n") {
		switch {
		case i == 0:
			b.WriteString(strings.TrimPrefix(line, prefix))
		case strings.HasPrefix(line, pad):
			b.WriteString(strings.TrimPrefix(line, pad))
		default:
			return b.String()
		}
	}
	return b.String()
}

// stripWhitespace removes all ASCII whitespace from s. Used by tests to
// compare wrapped output against the original command: wrapping may replace
// inter-token spaces with newlines and indent, so we compare only the
// non-whitespace runes.
func stripWhitespace(s string) string {
	s = strings.ReplaceAll(s, " ", "")
	s = strings.ReplaceAll(s, "\n", "")
	s = strings.ReplaceAll(s, "\t", "")
	return s
}

// TestRenderToolHeaderWrapsLongBashCommand ensures long bash commands are
// wrapped (not truncated with …) and every rendered line fits the width.
func TestRenderToolHeaderWrapsLongBashCommand(t *testing.T) {
	th := DefaultTheme()
	const width = 40
	// A long single-token command with no spaces exercises the hard-wrap path.
	cmd := "echo " + strings.Repeat("x", 120)
	item := Item{
		Kind: KindTool,
		Tool: &ToolBlock{
			Name:      "bash",
			Arguments: `{"command":"` + cmd + `"}`,
			Output:    "done\n",
			Done:      true,
		},
	}
	got := RenderItem(th, item, width, DefaultOutputLimit)

	prefix := th.ToolHeader.Render("⚙ bash") + " "
	indent := lipgloss.Width(prefix)
	content := reconstructHeader(got, prefix, indent)

	// The full command tail must appear; nothing width-truncated with ….
	if strings.Contains(got, "…") {
		t.Errorf("expected no ellipsis truncation for wrapped command, got:\n%s", got)
	}
	if !strings.Contains(got, "⚙ bash") {
		t.Errorf("missing bash header: %q", got)
	}
	if want := "$ echo " + strings.Repeat("x", 120); stripWhitespace(content) != stripWhitespace(want) {
		t.Errorf("command content not preserved:\nwant %q\ngot  %q", want, content)
	}

	for i, line := range strings.Split(got, "\n") {
		if w := lipgloss.Width(line); w > width {
			t.Errorf("line %d has width %d, exceeds %d: %q", i, w, width, line)
		}
	}

	// Continuation lines of the header must be indented to align under the
	// argument column (the width of the "⚙ bash " prefix).
	headerLines := strings.Split(got, "\n")
	if len(headerLines) < 2 {
		t.Fatalf("expected wrapped header to span multiple lines, got:\n%s", got)
	}
	cont := headerLines[1]
	if !strings.HasPrefix(cont, strings.Repeat(" ", indent)) {
		t.Errorf("continuation line not indented by %d cells: %q", indent, cont)
	}
}

// TestRenderToolHeaderWrapsAtWordBoundary ensures a bash command with spaces
// wraps at word boundaries first.
func TestRenderToolHeaderWrapsAtWordBoundary(t *testing.T) {
	th := DefaultTheme()
	const width = 24
	cmd := "git --no-pager log --oneline --graph --decorate --all -n 50"
	item := Item{
		Kind: KindTool,
		Tool: &ToolBlock{
			Name:      "bash",
			Arguments: `{"command":"` + cmd + `"}`,
			Output:    "ok\n",
			Done:      true,
		},
	}
	got := RenderItem(th, item, width, DefaultOutputLimit)
	for i, line := range strings.Split(got, "\n") {
		if w := lipgloss.Width(line); w > width {
			t.Errorf("line %d has width %d, exceeds %d: %q", i, w, width, line)
		}
	}
	// CLI flags must not be split at hyphens and the full command is preserved.
	prefix := th.ToolHeader.Render("⚙ bash") + " "
	indent := lipgloss.Width(prefix)
	content := reconstructHeader(got, prefix, indent)
	if want := "$ " + cmd; stripWhitespace(content) != stripWhitespace(want) {
		t.Errorf("command content not preserved:\nwant %q\ngot  %q", want, content)
	}
}

// TestRenderToolHeaderAsciiNoColor ensures wrapping renders without ANSI
// escapes in the no-color (Ascii) profile.
func TestRenderToolHeaderAsciiNoColor(t *testing.T) {
	orig := lipgloss.ColorProfile()
	defer lipgloss.SetColorProfile(orig)
	lipgloss.SetColorProfile(termenv.Ascii)

	th := DefaultTheme()
	cmd := "echo " + strings.Repeat("y", 80)
	item := Item{
		Kind: KindTool,
		Tool: &ToolBlock{
			Name:      "bash",
			Arguments: `{"command":"` + cmd + `"}`,
			Output:    "done\n",
			Done:      true,
		},
	}
	got := RenderItem(th, item, 30, DefaultOutputLimit)
	if strings.Contains(got, "\x1b[") {
		t.Errorf("expected no ANSI escapes in Ascii mode, got:\n%s", got)
	}
	prefix := th.ToolHeader.Render("⚙ bash") + " "
	indent := lipgloss.Width(prefix)
	if want := "$ " + cmd; stripWhitespace(reconstructHeader(got, prefix, indent)) != stripWhitespace(want) {
		t.Errorf("command content lost in no-color mode:\nwant %q\ngot  %q", want, reconstructHeader(got, prefix, indent))
	}
}

// TestWrapToolArgs covers the wrap helper directly: word-wrap then hard-wrap
// fallback, and continuation indent.
func TestWrapToolArgs(t *testing.T) {
	t.Run("fits on one line", func(t *testing.T) {
		got := wrapToolArgs("hello world", 80, 4)
		if got != "hello world" {
			t.Errorf("got %q", got)
		}
	})
	t.Run("word wrap indents continuation", func(t *testing.T) {
		got := wrapToolArgs("alpha beta gamma delta epsilon", 12, 3)
		lines := strings.Split(got, "\n")
		for i, line := range lines {
			if i == 0 {
				continue
			}
			if !strings.HasPrefix(line, "   ") {
				t.Errorf("line %d not indented: %q", i, line)
			}
			content := strings.TrimPrefix(line, "   ")
			if lipgloss.Width(content) > 12 {
				t.Errorf("line %d content width > 12: %q", i, line)
			}
		}
	})
	t.Run("long token hard wraps", func(t *testing.T) {
		got := wrapToolArgs(strings.Repeat("z", 25), 10, 0)
		for i, line := range strings.Split(got, "\n") {
			if lipgloss.Width(line) > 10 {
				t.Errorf("line %d exceeds 10: %q", i, line)
			}
		}
	})
	t.Run("width clamped to 1", func(t *testing.T) {
		got := wrapToolArgs("abc", 0, 0)
		if strings.Count(got, "\n") != 2 {
			t.Errorf("expected each rune on its own line, got %q", got)
		}
	})
}
