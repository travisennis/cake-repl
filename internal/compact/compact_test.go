package compact

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
)

// writeFakeCompactor writes an executable shell script standing in for
// cake-compact and returns its path.
func writeFakeCompactor(t *testing.T, script string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fake-cake-compact")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestArgs(t *testing.T) {
	args := Options{
		SessionID:         "11111111-2222-3333-4444-555555555555",
		CakeBin:           "/usr/local/bin/cake",
		ThresholdPercent:  80,
		KeepTokens:        20000,
		SummarizerModel:   "gpt-x",
		SummarizerProfile: "work",
	}.Args()
	want := []string{
		"compact", "-json", "-threshold",
		"-threshold-percent", "80",
		"-keep-tokens", "20000",
		"-cake-bin", "/usr/local/bin/cake",
		"-summarizer-model", "gpt-x",
		"-summarizer-profile", "work",
		"--", "11111111-2222-3333-4444-555555555555",
	}
	if !slices.Equal(args, want) {
		t.Fatalf("Args() = %v, want %v", args, want)
	}

	// Zero values are omitted so the compactor applies its own defaults.
	args = Options{SessionID: "s-1"}.Args()
	want = []string{"compact", "-json", "-threshold", "--", "s-1"}
	if !slices.Equal(args, want) {
		t.Fatalf("minimal Args() = %v, want %v", args, want)
	}
}

// TestArgsPrefersThresholdTokens checks that absolute mode passes exactly one
// threshold flag: the compactor's -threshold-tokens replaces the percent form
// even when both fields are populated.
func TestArgsPrefersThresholdTokens(t *testing.T) {
	args := Options{
		SessionID:        "11111111-2222-3333-4444-555555555555",
		ThresholdTokens:  350000,
		ThresholdPercent: 80,
		KeepTokens:       20000,
	}.Args()
	want := []string{
		"compact", "-json", "-threshold",
		"-threshold-tokens", "350000",
		"-keep-tokens", "20000",
		"--", "11111111-2222-3333-4444-555555555555",
	}
	if !slices.Equal(args, want) {
		t.Fatalf("Args() = %v, want %v", args, want)
	}
	for _, arg := range args {
		if arg == "-threshold-percent" {
			t.Fatalf("absolute mode also passed -threshold-percent: %v", args)
		}
	}
}

func TestCompactAppliesThresholdAndDecodesDocument(t *testing.T) {
	argsPath := filepath.Join(t.TempDir(), "args")
	t.Setenv("ARGS_FILE", argsPath)
	bin := writeFakeCompactor(t, `
printf '%s\n' "$@" >> "$ARGS_FILE"
cat <<'JSON'
{"compacted":true,"source_session_id":"s-1","session_id":"s-1","session_file":"/tmp/s-1.jsonl","in_place":true,"checkpoint_id":"c-1","boundary":7,"trigger":"manual","basis":"measured_usage","before_tokens":170000,"after_tokens":24000,"window":200000,"keep_target":20000,"prefix_items":12,"retained_items":5,"retained_count":4,"summary_chars":900,"transcript_chars":50000}
JSON
`)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	res := Compact(ctx, Options{
		Bin:              bin,
		SessionID:        "s-1",
		CakeBin:          "cake",
		ThresholdPercent: 80,
		KeepTokens:       20000,
	})

	if res.ExitCode != 0 || res.Err != nil || res.Canceled {
		t.Fatalf("result = %+v", res)
	}
	doc := res.Document
	if doc == nil {
		t.Fatal("no document decoded")
	}
	if !doc.Compacted || !doc.InPlace || doc.SessionID != "s-1" || doc.CheckpointID != "c-1" {
		t.Errorf("document identity = %+v", doc)
	}
	if doc.BeforeTokens != 170000 || doc.AfterTokens != 24000 || doc.Window != 200000 || doc.RetainedCount != 4 {
		t.Errorf("document numbers = %+v", doc)
	}
	raw, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Fields(string(raw))
	want := []string{
		"compact", "-json", "-threshold", "-threshold-percent", "80",
		"-keep-tokens", "20000", "-cake-bin", "cake", "--", "s-1",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("invoked args = %v, want %v", got, want)
	}
}

func TestCompactNoOpDocument(t *testing.T) {
	bin := writeFakeCompactor(t, `
cat <<'JSON'
{"compacted":false,"reason":"below threshold: 1000 < 160000 estimated input tokens","source_session_id":"s-1","before_tokens":1000,"prefix_items":0,"retained_items":0}
JSON
`)
	res := Compact(context.Background(), Options{Bin: bin, SessionID: "s-1", ThresholdPercent: 80})
	if res.ExitCode != 0 || res.Err != nil || res.Document == nil {
		t.Fatalf("result = %+v", res)
	}
	if res.Document.Compacted || !strings.Contains(res.Document.Reason, "below threshold") {
		t.Fatalf("document = %+v, want a no-op with a reason", res.Document)
	}
}

func TestCompactFailureReportsBoundedStderr(t *testing.T) {
	bin := writeFakeCompactor(t, `echo "cake sessions checkpoint is unavailable; upgrade cake" >&2; exit 1`)
	res := Compact(context.Background(), Options{Bin: bin, SessionID: "s-1"})
	if res.ExitCode != 1 {
		t.Fatalf("exit = %d, want 1", res.ExitCode)
	}
	if res.Document != nil || res.Err != nil || res.Canceled {
		t.Fatalf("result = %+v, want a plain nonzero exit", res)
	}
	if !strings.Contains(res.Stderr, "unavailable; upgrade cake") {
		t.Fatalf("stderr = %q", res.Stderr)
	}
}

func TestCompactMissingBinary(t *testing.T) {
	res := Compact(context.Background(), Options{
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

func TestCompactMalformedOutput(t *testing.T) {
	bin := writeFakeCompactor(t, `echo 'not a document'`)
	res := Compact(context.Background(), Options{Bin: bin, SessionID: "s-1"})
	if res.ExitCode != 0 || res.Err == nil {
		t.Fatalf("result = %+v, want a decode error", res)
	}
}

func TestCanceledExitRequiresSignalTermination(t *testing.T) {
	// A natural non-zero exit must stay a failure even when a Ctrl+C arrives
	// late: the cancel callback alone is not enough on POSIX.
	err := exec.Command("sh", "-c", "exit 3").Run()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("expected an ExitError, got %v", err)
	}
	if canceledExit(exitErr, false) {
		t.Fatal("an unsignaled exit was classified as canceled")
	}
	want := runtime.GOOS == "windows"
	if got := canceledExit(exitErr, true); got != want {
		t.Fatalf("canceledExit(exited, requested=true) = %v, want %v", got, want)
	}
}

func TestCompactCancellationReapsChild(t *testing.T) {
	bin := writeFakeCompactor(t, `exec sleep 30`)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	start := time.Now()
	res := Compact(ctx, Options{Bin: bin, SessionID: "s-1", waitDelay: 500 * time.Millisecond})
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

func TestCompactBoundsTeardownWhenDescendantHoldsStdout(t *testing.T) {
	bin := writeFakeCompactor(t, `sleep 5 & printf '{"compacted":false}'; exit 0`)
	start := time.Now()
	res := Compact(context.Background(), Options{Bin: bin, SessionID: "s-1", waitDelay: 100 * time.Millisecond})
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("teardown waited %s for a descendant", elapsed)
	}
	if res.Err == nil && res.Document == nil {
		t.Fatalf("result = %+v, want either the document or a teardown error", res)
	}
}

func TestCompactBoundsOutput(t *testing.T) {
	bin := writeFakeCompactor(t, `head -c 1100000 /dev/zero | tr '\0' x; exit 0`)
	res := Compact(context.Background(), Options{Bin: bin, SessionID: "s-1"})
	if res.Err == nil {
		t.Fatalf("result = %+v, want an output-size error", res)
	}
	if !strings.Contains(res.Err.Error(), "exceeded") {
		t.Fatalf("error = %v", res.Err)
	}
}
