package suggest

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// writeFakeSuggester writes an executable shell script standing in for
// cake-suggest and returns its path.
func writeFakeSuggester(t *testing.T, script string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fake-cake-suggest")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestArgs(t *testing.T) {
	args := Options{
		SessionID: "11111111-2222-3333-4444-555555555555",
		CakeBin:   "/usr/local/bin/cake",
		Model:     "gpt-x",
		Profile:   "work",
	}.Args()
	want := []string{
		"suggest", "-json",
		"-cake-bin", "/usr/local/bin/cake",
		"-model", "gpt-x",
		"-profile", "work",
		"--", "11111111-2222-3333-4444-555555555555",
	}
	if !slices.Equal(args, want) {
		t.Fatalf("Args() = %v, want %v", args, want)
	}

	// Zero values are omitted so the suggester applies its own defaults.
	args = Options{SessionID: "s-1"}.Args()
	want = []string{"suggest", "-json", "--", "s-1"}
	if !slices.Equal(args, want) {
		t.Fatalf("minimal Args() = %v, want %v", args, want)
	}
}

func TestSuggestDecodesDocument(t *testing.T) {
	argsPath := filepath.Join(t.TempDir(), "args")
	t.Setenv("ARGS_FILE", argsPath)
	bin := writeFakeSuggester(t, `
printf '%s\n' "$@" >> "$ARGS_FILE"
cat <<'JSON'
{"suggested":true,"suggestion":"Add a test for the new path.","session_id":"s-1","reason":"","new_field":true}
JSON
`)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	res := Suggest(ctx, Options{
		Bin:       bin,
		SessionID: "s-1",
		CakeBin:   "cake",
		Model:     "gpt-x",
		Profile:   "work",
	})

	if res.ExitCode != 0 || res.Err != nil || res.Canceled {
		t.Fatalf("result = %+v", res)
	}
	doc := res.Document
	if doc == nil {
		t.Fatal("no document decoded")
	}
	if !doc.Suggested || doc.Suggestion != "Add a test for the new path." || doc.SessionID != "s-1" {
		t.Errorf("document = %+v", doc)
	}
	raw, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Fields(string(raw))
	want := []string{
		"suggest", "-json", "-cake-bin", "cake", "-model", "gpt-x",
		"-profile", "work", "--", "s-1",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("invoked args = %v, want %v", got, want)
	}
}

func TestSuggestNoOpDocument(t *testing.T) {
	bin := writeFakeSuggester(t, `
cat <<'JSON'
{"suggested":false,"reason":"no completed turn","session_id":"s-1"}
JSON
`)
	res := Suggest(context.Background(), Options{Bin: bin, SessionID: "s-1"})
	if res.ExitCode != 0 || res.Err != nil || res.Document == nil {
		t.Fatalf("result = %+v", res)
	}
	if res.Document.Suggested || !strings.Contains(res.Document.Reason, "no completed turn") {
		t.Fatalf("document = %+v, want a no-op with a reason", res.Document)
	}
}

func TestSuggestFailureReportsBoundedStderr(t *testing.T) {
	bin := writeFakeSuggester(t, `echo "suggester cannot read the session" >&2; exit 1`)
	res := Suggest(context.Background(), Options{Bin: bin, SessionID: "s-1"})
	if res.ExitCode != 1 {
		t.Fatalf("exit = %d, want 1", res.ExitCode)
	}
	if res.Document != nil || res.Err != nil || res.Canceled {
		t.Fatalf("result = %+v, want a plain nonzero exit", res)
	}
	if !strings.Contains(res.Stderr, "cannot read the session") {
		t.Fatalf("stderr = %q", res.Stderr)
	}
}

func TestSuggestMissingBinary(t *testing.T) {
	res := Suggest(context.Background(), Options{
		Bin:       filepath.Join(t.TempDir(), "does-not-exist"),
		SessionID: "s-1",
	})
	if res.Err == nil {
		t.Fatalf("result = %+v, want a start error", res)
	}
	if res.Document != nil || res.Canceled {
		t.Fatalf("result = %+v", res)
	}
}

func TestSuggestMalformedOutput(t *testing.T) {
	bin := writeFakeSuggester(t, `echo 'not a document'`)
	res := Suggest(context.Background(), Options{Bin: bin, SessionID: "s-1"})
	if res.ExitCode != 0 || res.Err == nil {
		t.Fatalf("result = %+v, want a decode error", res)
	}
}

func TestSuggestCancellationReapsChild(t *testing.T) {
	bin := writeFakeSuggester(t, `exec sleep 30`)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	start := time.Now()
	res := Suggest(ctx, Options{Bin: bin, SessionID: "s-1", waitDelay: 500 * time.Millisecond})
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("cancellation took %s", elapsed)
	}
	if !res.Canceled {
		t.Fatalf("result = %+v, want canceled", res)
	}
	if res.Document != nil {
		t.Fatalf("canceled run produced a document: %+v", res.Document)
	}
}

func TestSuggestBoundsTeardownWhenDescendantHoldsStdout(t *testing.T) {
	bin := writeFakeSuggester(t, `sleep 5 & printf '{"suggested":false}'; exit 0`)
	start := time.Now()
	res := Suggest(context.Background(), Options{Bin: bin, SessionID: "s-1", waitDelay: 100 * time.Millisecond})
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("teardown waited %s for a descendant", elapsed)
	}
	if res.Err == nil && res.Document == nil {
		t.Fatalf("result = %+v, want either the document or a teardown error", res)
	}
}

func TestSuggestBoundsOutput(t *testing.T) {
	bin := writeFakeSuggester(t, `head -c 70000 /dev/zero | tr '\0' x; exit 0`)
	res := Suggest(context.Background(), Options{Bin: bin, SessionID: "s-1"})
	if res.Err == nil {
		t.Fatalf("result = %+v, want an output-size error", res)
	}
	if !strings.Contains(res.Err.Error(), "exceeded") {
		t.Fatalf("error = %v", res.Err)
	}
}
