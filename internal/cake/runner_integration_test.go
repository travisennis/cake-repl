//go:build integration

package cake

import (
	"os"
	"os/exec"
	"testing"
	"time"
)

// TestSmokeRealCake drives the real cake binary end to end and checks the
// stream-json contract the REPL depends on: a task_start with ids, a
// non-error task_complete, no unparseable lines, and a clean exit.
//
// WARNING: this spawns the actual cake CLI, which issues a model-backed
// request and can cost money. It is opt-in twice over — the `integration`
// build tag plus CAKE_REAL_SMOKE=1 — so it never runs from `just test`,
// `just test-race`, or `just ci`. Use `just test-real-cake`.
func TestSmokeRealCake(t *testing.T) {
	if os.Getenv("CAKE_REAL_SMOKE") != "1" {
		t.Skip("set CAKE_REAL_SMOKE=1 to run the real-cake smoke test; it makes a model-backed cake request")
	}
	bin := os.Getenv("CAKE_BIN")
	if bin == "" {
		bin = "cake"
	}
	if _, err := exec.LookPath(bin); err != nil {
		t.Skipf("cake binary %q not found; set CAKE_BIN or add cake to PATH", bin)
	}

	run, err := Start(Options{Bin: bin, Prompt: "Reply with the single word: ok"})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	// Do not leave a real cake process running if the drain times out.
	t.Cleanup(run.Cancel)

	events, res := collectWithin(t, run, 30*time.Second)

	if res.ExitCode != 0 || res.Canceled || res.Err != nil {
		t.Fatalf("unexpected result: %+v (stderr: %s)", res, res.Stderr)
	}
	if len(events) < 2 {
		t.Fatalf("got %d events, want at least task_start and task_complete: %#v", len(events), events)
	}
	for i, ev := range events {
		if pe, ok := ev.(ParseError); ok {
			t.Errorf("event %d did not parse: line=%q err=%v", i, pe.Line, pe.Err)
		}
	}

	start, ok := events[0].(TaskStart)
	if !ok {
		t.Fatalf("event 0 is %T, want TaskStart", events[0])
	}
	if start.SessionID == "" || start.TaskID == "" {
		t.Errorf("task_start is missing ids: %+v", start)
	}

	last := events[len(events)-1]
	done, ok := last.(TaskComplete)
	if !ok {
		t.Fatalf("last event is %T, want TaskComplete", last)
	}
	if done.IsError {
		t.Errorf("task completed with an error: %+v", done)
	}
	if done.SessionID != start.SessionID {
		t.Errorf("task_complete session %q does not match task_start %q", done.SessionID, start.SessionID)
	}
}
