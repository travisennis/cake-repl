package ui

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestSummarizeBash(t *testing.T) {
	got := SummarizeToolArgs("bash", `{"command":"ls -la","timeout":30}`)
	if got != "$ ls -la" {
		t.Errorf("got %q", got)
	}
}

func TestSummarizeBashWithCwd(t *testing.T) {
	got := SummarizeToolArgs("bash", `{"command":"make","cwd":"/tmp/x"}`)
	if !strings.Contains(got, "$ make") || !strings.Contains(got, "cwd: /tmp/x") {
		t.Errorf("got %q", got)
	}
}

func TestSummarizeBashMultilineCommand(t *testing.T) {
	got := SummarizeToolArgs("bash", `{"command":"echo one\necho two"}`)
	if strings.Contains(got, "\n") {
		t.Errorf("multiline command should be flattened, got %q", got)
	}
}

func TestSummarizeReadWithRange(t *testing.T) {
	got := SummarizeToolArgs("read", `{"path":"main.go","start_line":10,"end_line":20}`)
	if got != "main.go (lines 10-20)" {
		t.Errorf("got %q", got)
	}
}

func TestSummarizeReadPathOnly(t *testing.T) {
	got := SummarizeToolArgs("Read", `{"path":"main.go"}`)
	if got != "main.go" {
		t.Errorf("got %q (tool name matching should be case-insensitive)", got)
	}
}

func TestSummarizeEdit(t *testing.T) {
	got := SummarizeToolArgs("edit", `{"path":"a.go","edits":[{"old_text":"foo","new_text":"bar"},{"old_text":"x","new_text":"y"}]}`)
	if !strings.Contains(got, "a.go (2 edits)") {
		t.Errorf("missing path/count: %q", got)
	}
	if !strings.Contains(got, "foo → bar") {
		t.Errorf("missing edit preview: %q", got)
	}
}

func TestSummarizeEditCapsPreviews(t *testing.T) {
	got := SummarizeToolArgs("edit", `{"path":"a.go","edits":[`+
		`{"old_text":"1","new_text":"a"},{"old_text":"2","new_text":"b"},`+
		`{"old_text":"3","new_text":"c"},{"old_text":"4","new_text":"d"},`+
		`{"old_text":"5","new_text":"e"}]}`)
	if !strings.Contains(got, "… 2 more") {
		t.Errorf("expected preview cap marker: %q", got)
	}
}

func TestSummarizeWrite(t *testing.T) {
	got := SummarizeToolArgs("write", `{"path":"out.txt","content":"\nfirst real line\nsecond\n"}`)
	if !strings.Contains(got, "out.txt") || !strings.Contains(got, "3 lines") {
		t.Errorf("got %q", got)
	}
	if !strings.Contains(got, `"first real line"`) {
		t.Errorf("missing first non-empty line: %q", got)
	}
}

func TestSummarizeUnknownToolTruncates(t *testing.T) {
	long := `{"key":"` + strings.Repeat("v", 500) + `"}`
	got := SummarizeToolArgs("mystery_tool", long)
	if len(got) > unknownArgsLimit+len("…") {
		t.Errorf("summary too long: %d chars", len(got))
	}
}

func TestSummarizeBadArgsFallsBack(t *testing.T) {
	got := SummarizeToolArgs("bash", `not json`)
	if got != "not json" {
		t.Errorf("got %q", got)
	}
}

func TestTruncateOutputShortUnchanged(t *testing.T) {
	if got := TruncateOutput("short", 100); got != "short" {
		t.Errorf("got %q", got)
	}
}

func TestTruncateStringIsCellAware(t *testing.T) {
	// 8 CJK runes = 16 cells; a budget of 8 cells fits three runes plus the
	// one-cell ellipsis.
	if got := truncateString("日本語のテキスト", 8); got != "日本語…" {
		t.Errorf("got %q", got)
	}
	if got := truncateString("hello", 8); got != "hello" {
		t.Errorf("short string should be unchanged, got %q", got)
	}
}

func TestTruncateOutputDoesNotSplitRunes(t *testing.T) {
	in := strings.Repeat("é", 100) // 200 bytes, no newlines
	got := TruncateOutput(in, 7)   // 7 lands mid-rune
	if !utf8.ValidString(got) {
		t.Fatalf("output is not valid UTF-8: %q", got)
	}
	if !strings.HasPrefix(got, "ééé\n") {
		t.Errorf("expected cut after 3 full runes, got %q", got)
	}
}

func TestTruncateOutputPrefersLineBoundary(t *testing.T) {
	var b strings.Builder
	for range 100 {
		b.WriteString(strings.Repeat("x", 39) + "\n")
	}
	in := b.String() // 4000 bytes
	got := TruncateOutput(in, 2000)

	if !strings.Contains(got, "truncated") {
		t.Fatalf("missing truncation marker: %q", got[len(got)-80:])
	}
	if !strings.Contains(got, "3999 bytes") { // trailing newline trimmed
		t.Errorf("marker should report original size: %q", got[len(got)-80:])
	}
	lines := strings.Split(got, "\n")
	// Every content line should be a full 39-char line (cut at a boundary).
	for i, line := range lines[:len(lines)-1] {
		if len(line) != 39 {
			t.Errorf("line %d not cut at boundary: %q", i, line)
		}
	}
}
