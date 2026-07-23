package app

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
