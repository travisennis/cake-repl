package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/travisennis/cake-repl/internal/ui"
)

func TestLoadHistoryNormal(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "history")

	content := "first\nsecond\nthird\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	m := New(Config{HistoryFile: path})
	want := []string{"first", "second", "third"}
	if len(m.history.entries) != len(want) {
		t.Fatalf("len(entries) = %d, want %d", len(m.history.entries), len(want))
	}
	for i, e := range m.history.entries {
		if e != want[i] {
			t.Errorf("entries[%d] = %q, want %q", i, e, want[i])
		}
	}
	if m.history.idx != len(want) {
		t.Errorf("idx = %d, want %d", m.history.idx, len(want))
	}
}

func TestLoadHistoryOverCap(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "history")

	// Write maxHistoryEntries + 5 entries with distinguishable names so we can
	// verify the *last* N survive trimming, not just the count.
	var lines []string
	for i := range maxHistoryEntries + 5 {
		lines = append(lines, fmt.Sprintf("entry-%d", i))
	}
	content := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	m := New(Config{HistoryFile: path})

	if len(m.history.entries) != maxHistoryEntries {
		t.Fatalf("len(entries) = %d, want %d", len(m.history.entries), maxHistoryEntries)
	}

	// The first 5 entries (entry-0 … entry-4) should have been trimmed.
	if m.history.entries[0] != "entry-5" {
		t.Fatalf("first entry = %q, want %q (expected trim of oldest 5)",
			m.history.entries[0], "entry-5")
	}

	// The rewrite should produce a file where every line starts with jsonlPrefix.
	rewritten, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	rewrittenLines := strings.Split(strings.TrimSuffix(string(rewritten), "\n"), "\n")
	if len(rewrittenLines) != maxHistoryEntries {
		t.Fatalf("rewritten file has %d lines, want %d", len(rewrittenLines), maxHistoryEntries)
	}
	for i, line := range rewrittenLines {
		if !strings.HasPrefix(line, jsonlPrefix) {
			t.Errorf("line %d %q is missing the JSONL marker", i, line)
		}
	}

	m2 := New(Config{HistoryFile: path})
	if len(m2.history.entries) != maxHistoryEntries {
		t.Fatalf("after reload: len(entries) = %d, want %d", len(m2.history.entries), maxHistoryEntries)
	}
	if m2.history.entries[0] != "entry-5" {
		t.Fatalf("after reload: first entry = %q, want %q", m2.history.entries[0], "entry-5")
	}
}

func TestLoadHistoryEmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "history")

	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	m := New(Config{HistoryFile: path})
	if len(m.history.entries) != 0 {
		t.Errorf("entries = %v, want empty", m.history.entries)
	}
	if m.history.idx != 0 {
		t.Errorf("idx = %d, want 0", m.history.idx)
	}
}

func TestLoadHistoryNonexistentFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nonexistent")

	m := New(Config{HistoryFile: path})
	if len(m.history.entries) != 0 {
		t.Errorf("entries = %v, want empty", m.history.entries)
	}
	if m.history.idx != 0 {
		t.Errorf("idx = %d, want 0", m.history.idx)
	}
}

func TestLoadHistoryMultilineRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "history")

	// Simulate the normal session flow: write entries via persistHistory,
	// then load them back on the next start.
	cfg := Config{HistoryFile: path}
	m := New(cfg)

	prompts := []string{
		"single-line",
		"line1\nline2",
		"line1\nline2\nline3",
		"contains \"quotes\" and \\backslashes",
	}
	for _, p := range prompts {
		m.persistHistory(p)
	}

	// Verify every line in the file has the jsonlPrefix and is valid JSON.
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n")
	if len(lines) != len(prompts) {
		t.Fatalf("file has %d lines, want %d", len(lines), len(prompts))
	}
	for i, line := range lines {
		if !strings.HasPrefix(line, jsonlPrefix) {
			t.Errorf("line %d %q is missing the JSONL marker", i, line)
			continue
		}
		var s string
		if err := json.Unmarshal([]byte(line[len(jsonlPrefix):]), &s); err != nil {
			t.Errorf("line %d %q is not valid JSON: %v", i, line, err)
		}
	}

	// Now load from a fresh model, as if starting a new session.
	m2 := New(cfg)
	if len(m2.history.entries) != len(prompts) {
		t.Fatalf("len(entries) = %d, want %d", len(m2.history.entries), len(prompts))
	}
	for i, want := range prompts {
		if m2.history.entries[i] != want {
			t.Errorf("entries[%d] = %q, want %q", i, m2.history.entries[i], want)
		}
	}
}

func TestLoadHistoryLegacyQuotedString(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "history")

	// A legacy line that looks like a valid JSON string should be treated as
	// plain text (no jsonlPrefix → not decoded).
	content := "\"hello world\"\nplain\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	m := New(Config{HistoryFile: path})
	want := []string{"\"hello world\"", "plain"}
	if len(m.history.entries) != len(want) {
		t.Fatalf("len(entries) = %d, want %d", len(m.history.entries), len(want))
	}
	for i, e := range m.history.entries {
		if e != want[i] {
			t.Errorf("entries[%d] = %q, want %q", i, e, want[i])
		}
	}
}

func TestLoadHistoryMixedFormat(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "history")

	// A file with a mix of jsonlPrefix-marked entries and plain-text lines.
	content := jsonlPrefix + "\"json entry\"\n" +
		"raw plain text\n" +
		jsonlPrefix + "\"another json\"\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	m := New(Config{HistoryFile: path})
	want := []string{"json entry", "raw plain text", "another json"}
	if len(m.history.entries) != len(want) {
		t.Fatalf("len(entries) = %d, want %d", len(m.history.entries), len(want))
	}
	for i, e := range m.history.entries {
		if e != want[i] {
			t.Errorf("entries[%d] = %q, want %q", i, e, want[i])
		}
	}
}

func TestLoadHistoryLongLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "history")

	longLine := strings.Repeat("x", 80*1024)
	content := longLine + "\nshort\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	m := New(Config{HistoryFile: path})

	if len(m.history.entries) != 2 {
		t.Fatalf("len(entries) = %d, want 2", len(m.history.entries))
	}
	if len(m.history.entries[0]) != 80*1024 {
		t.Fatalf("long entry length = %d, want %d", len(m.history.entries[0]), 80*1024)
	}
	if m.history.entries[1] != "short" {
		t.Fatalf("entries[1] = %q, want %q", m.history.entries[1], "short")
	}
	if m.history.idx != 2 {
		t.Errorf("idx = %d, want 2", m.history.idx)
	}
}

func TestLoadHistorySkipsBlankLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "history")

	content := "first\n\nsecond\n\n\nthird\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	m := New(Config{HistoryFile: path})
	want := []string{"first", "second", "third"}
	if len(m.history.entries) != len(want) {
		t.Fatalf("len(entries) = %d, want %d", len(m.history.entries), len(want))
	}
	for i, e := range m.history.entries {
		if e != want[i] {
			t.Errorf("entries[%d] = %q, want %q", i, e, want[i])
		}
	}
}

// TestLoadHistorySkipsOversizedRecord covers the oversized-record recovery
// policy: a record line over maxHistoryRecordBytes is drained and skipped
// (bounded memory, resuming at the next record) so the valid records around it
// stay usable, the on-disk file is not rewritten, and a content-free warning
// reports the drop.
func TestLoadHistorySkipsOversizedRecord(t *testing.T) {
	oversized := strings.Repeat("x", maxHistoryRecordBytes+16)
	cases := []struct {
		name    string
		content string
		want    []string
	}{
		{
			name:    "middle",
			content: encodeHistoryEntry("first") + oversized + "\n" + encodeHistoryEntry("last"),
			want:    []string{"first", "last"},
		},
		{
			name:    "start",
			content: oversized + "\n" + encodeHistoryEntry("after"),
			want:    []string{"after"},
		},
		{
			name:    "end with newline",
			content: encodeHistoryEntry("before") + oversized + "\n",
			want:    []string{"before"},
		},
		{
			name:    "end without newline",
			content: encodeHistoryEntry("before") + oversized,
			want:    []string{"before"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "history")
			if err := os.WriteFile(path, []byte(tc.content), 0o600); err != nil {
				t.Fatal(err)
			}

			m := New(Config{HistoryFile: path})
			if !reflect.DeepEqual(m.history.entries, tc.want) {
				t.Fatalf("entries = %q, want %q", m.history.entries, tc.want)
			}
			if m.history.idx != len(tc.want) {
				t.Errorf("idx = %d, want %d", m.history.idx, len(tc.want))
			}

			warn, ok := warningItem(m)
			if !ok {
				t.Fatal("no warning reported the skipped oversized record")
			}
			if !strings.Contains(warn.Text, "oversized") {
				t.Errorf("warning = %q, want an oversized-record notice", warn.Text)
			}
			if strings.Contains(warn.Text, "xxxx") {
				t.Errorf("warning leaked the dropped record content: %q", warn.Text)
			}

			// The load must not rewrite (wipe) the file: the oversized record is
			// still on disk byte-for-byte, and a reload behaves the same.
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(data) != tc.content {
				t.Error("load rewrote the history file despite a skipped record")
			}
			if again := New(Config{HistoryFile: path}); !reflect.DeepEqual(again.history.entries, tc.want) {
				t.Errorf("reload entries = %q, want %q", again.history.entries, tc.want)
			}
		})
	}
}

// TestPersistHistorySizeBoundary pins the writer/reader size contract: a
// record whose encoded line is exactly maxHistoryRecordBytes is written and
// read back, while one byte more is dropped (reported oversized) so it can
// never be persisted into a record loadHistory would skip.
func TestPersistHistorySizeBoundary(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "history")
	m := New(Config{HistoryFile: path})

	// encodeHistoryEntry adds the \x02 prefix, the two JSON quotes, and the
	// newline, so the text is four bytes shorter than the encoded record.
	atLimit := strings.Repeat("x", maxHistoryRecordBytes-4)
	if m.persistHistory(atLimit) {
		t.Error("entry at the size limit was reported oversized")
	}
	overLimit := strings.Repeat("x", maxHistoryRecordBytes-3)
	if !m.persistHistory(overLimit) {
		t.Error("entry over the size limit was not reported oversized")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(string(data), "\n"); got != 1 {
		t.Fatalf("file has %d records, want 1", got)
	}
	m2 := New(Config{HistoryFile: path})
	if len(m2.history.entries) != 1 || m2.history.entries[0] != atLimit {
		t.Fatalf("reloaded entries = %d, want the at-limit record of %d bytes", len(m2.history.entries), len(atLimit))
	}
}

// TestSubmitDropsOversizedHistoryEntryWithWarning exercises the submit path: a
// large pasted slash input must not be written to history, and a content-free
// warning on the timeline must report the drop without echoing prompt text.
func TestSubmitDropsOversizedHistoryEntryWithWarning(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "history")
	m := New(Config{HistoryFile: path})
	m.width, m.height = 80, 24
	m.layout()

	const marker = "LEAK-MARKER"
	m.input.SetValue("/help " + marker + strings.Repeat("x", maxHistoryRecordBytes))
	tm, _ := m.submit()
	got := tm.(Model)

	// The oversized record was dropped, not written.
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	if strings.Contains(string(data), marker) {
		t.Fatal("oversized prompt was written to the history file")
	}

	warn, ok := warningItem(got)
	if !ok {
		t.Fatal("no warning reported the dropped history record")
	}
	if !strings.Contains(warn.Text, "too large") {
		t.Errorf("warning = %q, want a too-large notice", warn.Text)
	}
	if strings.Contains(warn.Text, marker) || strings.Contains(warn.Text, "xxxx") {
		t.Errorf("warning leaked prompt content: %q", warn.Text)
	}
	// The command itself still ran: the help text reached the timeline.
	if !timelineContains(got, HelpText) {
		t.Error("the slash command did not run after the history drop")
	}
}

// TestReadHistoryKeepsEntriesBeforeReadError proves a mid-file read error
// still yields the entries read so far (and returns the error), so a caller
// never discards valid history just because the tail failed.
func TestReadHistoryKeepsEntriesBeforeReadError(t *testing.T) {
	r := &errAfterReader{data: []byte("first\n"), err: errors.New("boom")}
	entries, skipped, err := readHistory(r)
	if err == nil {
		t.Fatal("err = nil, want the read error")
	}
	if skipped != 0 {
		t.Errorf("skipped = %d, want 0", skipped)
	}
	if !reflect.DeepEqual(entries, []string{"first"}) {
		t.Errorf("entries = %q, want [first]", entries)
	}
}

// errAfterReader yields data and then fails, standing in for a file that
// errors mid-read.
type errAfterReader struct {
	data []byte
	err  error
}

func (r *errAfterReader) Read(p []byte) (int, error) {
	if len(r.data) == 0 {
		return 0, r.err
	}
	n := copy(p, r.data)
	r.data = r.data[n:]
	return n, nil
}

// warningItem returns the first warning timeline item.
func warningItem(m Model) (ui.Item, bool) {
	for _, it := range m.items {
		if it.Kind == ui.KindWarning {
			return it, true
		}
	}
	return ui.Item{}, false
}

// timelineContains reports whether any timeline item's text is exactly want.
func timelineContains(m Model, want string) bool {
	for _, it := range m.items {
		if it.Text == want {
			return true
		}
	}
	return false
}
