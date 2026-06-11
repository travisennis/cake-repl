package cake

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeFakeCake writes an executable shell script standing in for the cake
// binary and returns its path.
func writeFakeCake(t *testing.T, script string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fake-cake")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// collect drains a run's events and final result with a timeout.
func collect(t *testing.T, run *Run) ([]Event, Result) {
	t.Helper()
	var events []Event
	timeout := time.After(10 * time.Second)
	for {
		select {
		case ev, ok := <-run.Events:
			if !ok {
				select {
				case res := <-run.Result:
					return events, res
				case <-timeout:
					t.Fatal("timed out waiting for result")
				}
			}
			events = append(events, ev)
		case <-timeout:
			t.Fatal("timed out waiting for events")
		}
	}
}

func TestRunnerHappyPath(t *testing.T) {
	bin := writeFakeCake(t, `
cat <<'EOF'
{"type":"task_start","session_id":"11111111-2222-3333-4444-555555555555","task_id":"t-1","timestamp":"2026-06-09T12:00:00Z"}
{"type":"message","role":"assistant","content":"hello there"}
{"type":"function_call","id":"fc-1","call_id":"c-1","name":"bash","arguments":"{\"command\":\"ls\"}"}
{"type":"function_call_output","call_id":"c-1","output":"file.txt"}
{"type":"task_complete","subtype":"success","is_error":false,"duration_ms":10,"turn_count":1,"tool_call_count":1,"session_id":"11111111-2222-3333-4444-555555555555","task_id":"t-1","usage":{"input_tokens":1,"output_tokens":2,"total_tokens":3}}
EOF
exit 0
`)
	run, err := Start(Options{Bin: bin, Prompt: "hi"})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	events, res := collect(t, run)

	if len(events) != 5 {
		t.Fatalf("got %d events, want 5: %#v", len(events), events)
	}
	if _, ok := events[0].(TaskStart); !ok {
		t.Errorf("event 0 is %T, want TaskStart", events[0])
	}
	if _, ok := events[4].(TaskComplete); !ok {
		t.Errorf("event 4 is %T, want TaskComplete", events[4])
	}
	if res.ExitCode != 0 || res.Canceled || res.Err != nil {
		t.Errorf("unexpected result: %+v", res)
	}
}

func TestRunnerMalformedLineKeepsStreaming(t *testing.T) {
	bin := writeFakeCake(t, `
cat <<'EOF'
{"type":"message","role":"assistant","content":"before"}
this is not json at all
{"type":"message","role":"assistant","content":"after"}
EOF
exit 0
`)
	run, err := Start(Options{Bin: bin, Prompt: "hi"})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	events, res := collect(t, run)

	if len(events) != 3 {
		t.Fatalf("got %d events, want 3: %#v", len(events), events)
	}
	pe, ok := events[1].(ParseError)
	if !ok {
		t.Fatalf("event 1 is %T, want ParseError", events[1])
	}
	if !strings.Contains(pe.Line, "not json") {
		t.Errorf("diagnostic should preserve raw line, got %q", pe.Line)
	}
	if msg, ok := events[2].(Message); !ok || msg.Content != "after" {
		t.Errorf("stream did not continue past malformed line: %#v", events[2])
	}
	if res.ExitCode != 0 {
		t.Errorf("unexpected exit code %d", res.ExitCode)
	}
}

func TestRunnerNonZeroExitWithStderr(t *testing.T) {
	bin := writeFakeCake(t, `
echo "something broke" >&2
exit 2
`)
	run, err := Start(Options{Bin: bin, Prompt: "hi"})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	events, res := collect(t, run)

	if len(events) != 0 {
		t.Errorf("expected no events, got %#v", events)
	}
	if res.ExitCode != 2 {
		t.Errorf("exit code = %d, want 2", res.ExitCode)
	}
	if !strings.Contains(res.Stderr, "something broke") {
		t.Errorf("stderr not captured: %q", res.Stderr)
	}
	if res.Canceled {
		t.Error("run should not be marked canceled")
	}
}

func TestRunnerCancellation(t *testing.T) {
	bin := writeFakeCake(t, `
echo '{"type":"task_start","session_id":"s","task_id":"t","timestamp":"2026-06-09T12:00:00Z"}'
sleep 30
`)
	run, err := Start(Options{Bin: bin, Prompt: "hi"})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Wait for the first event so we know the process is alive.
	select {
	case ev := <-run.Events:
		if _, ok := ev.(TaskStart); !ok {
			t.Fatalf("got %T, want TaskStart", ev)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for task_start")
	}

	start := time.Now()
	run.Cancel()
	_, res := collect(t, run)

	if !res.Canceled {
		t.Errorf("result not marked canceled: %+v", res)
	}
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Errorf("cancellation took too long: %v", elapsed)
	}
}

func TestRunnerMissingBinary(t *testing.T) {
	_, err := Start(Options{Bin: filepath.Join(t.TempDir(), "nope"), Prompt: "hi"})
	if err == nil {
		t.Fatal("want error for missing binary")
	}
}

func TestRunnerExitBeforeComplete(t *testing.T) {
	bin := writeFakeCake(t, `
echo '{"type":"message","role":"assistant","content":"partial"}'
exit 1
`)
	run, err := Start(Options{Bin: bin, Prompt: "hi"})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	events, res := collect(t, run)
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}
	if res.ExitCode != 1 {
		t.Errorf("exit code = %d, want 1", res.ExitCode)
	}
}

func TestTailBuffer(t *testing.T) {
	tests := []struct {
		name   string
		limit  int
		writes []string
		want   string
	}{
		{"under limit", 8, []string{"  hi\n"}, "hi"},
		{"rolls across writes", 8, []string{"aaaa", "bbbb", "cccc"}, "…bbbbcccc"},
		{"single write over limit", 4, []string{"0123456789"}, "…6789"},
		{"empty", 8, nil, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := &tailBuffer{limit: tt.limit}
			for _, w := range tt.writes {
				n, err := b.Write([]byte(w))
				if n != len(w) || err != nil {
					t.Fatalf("Write(%q) = (%d, %v), want (%d, nil)", w, n, err, len(w))
				}
			}
			if got := b.String(); got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
			if len(b.buf) > tt.limit {
				t.Errorf("retained %d bytes, limit %d", len(b.buf), tt.limit)
			}
		})
	}
}

func TestOptionsArgs(t *testing.T) {
	tests := []struct {
		name string
		opts Options
		want []string
	}{
		{
			name: "fresh",
			opts: Options{Prompt: "hi", Mode: RunFresh},
			want: []string{"--output-format", "stream-json", "--", "hi"},
		},
		{
			name: "continue",
			opts: Options{Prompt: "hi", Mode: RunContinue},
			want: []string{"--output-format", "stream-json", "--continue", "--", "hi"},
		},
		{
			name: "resume",
			opts: Options{Prompt: "hi", Mode: RunResume, ResumeID: "abc"},
			want: []string{"--output-format", "stream-json", "--resume", "abc", "--", "hi"},
		},
		{
			name: "passthrough",
			opts: Options{Prompt: "hi", Model: "gpt-x", Profile: "fast"},
			want: []string{"--output-format", "stream-json", "--model", "gpt-x", "--profile", "fast", "--", "hi"},
		},
		{
			name: "flag-like prompt",
			opts: Options{Prompt: "-v means what in grep?", Mode: RunFresh},
			want: []string{"--output-format", "stream-json", "--", "-v means what in grep?"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.opts.Args()
			if len(got) != len(tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("got %v, want %v", got, tt.want)
				}
			}
		})
	}
}
