package app

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/travisennis/cake-repl/internal/cake"
	"github.com/travisennis/cake-repl/internal/childproc"
)

// TestCancelRunningWaitsForReplayStart covers the replay command before its
// replayStartedMsg reaches Update: the model has no *cake.Run yet, but the
// model-owned context and done channel still stop and reap the child.
func TestCancelRunningWaitsForReplayStart(t *testing.T) {
	started := filepath.Join(t.TempDir(), "started")
	t.Setenv("STARTED_FILE", started)
	bin := writeFakeCake(t, `
touch "$STARTED_FILE"
trap 'exit 0' TERM
while :; do sleep 0.05; done
`)

	m := New(Config{
		CakeBin:  bin,
		Cwd:      t.TempDir(),
		ResumeID: replayTestSessionID,
		Children: childproc.NewRegistry(),
	})
	start := replayStartMessage(t, m)
	if start.err != nil || start.run == nil {
		t.Fatalf("replay start = %+v, want a running child", start)
	}
	waitForFile(t, started)

	if err := m.CancelRunning(); err != nil {
		t.Fatalf("CancelRunning: %v", err)
	}
	select {
	case <-start.run.Done:
	default:
		t.Fatal("CancelRunning returned before the replay was reaped")
	}
	if res := <-start.run.Result; !res.Canceled {
		t.Fatalf("replay result = %+v, want Canceled", res)
	}
}

// TestCancelRunningWaitsForCompactionTeardown covers the between-turn compactor
// draining while the Update loop is gone.
func TestCancelRunningWaitsForCompactionTeardown(t *testing.T) {
	started := filepath.Join(t.TempDir(), "started")
	t.Setenv("STARTED_FILE", started)
	bin := writeFakeCompactor(t, `
touch "$STARTED_FILE"
trap 'exit 0' TERM
while :; do sleep 0.05; done
`)

	m := compactionModel(t, bin)
	m.cfg.Children = childproc.NewRegistry()
	tm, cmd := m.finishRun(cake.Result{ExitCode: 0})
	m = tm.(Model)
	done := m.compaction.doneChannel()
	if cmd == nil || done == nil {
		t.Fatal("finishRun did not leave a bounded compactor child")
	}
	result := runCompactionCmd(cmd)
	waitForFile(t, started)

	if err := m.CancelRunning(); err != nil {
		t.Fatalf("CancelRunning: %v", err)
	}
	select {
	case <-done:
	default:
		t.Fatal("CancelRunning returned before the compactor was reaped")
	}
	if msg := waitForCompaction(t, result); msg.req == 0 {
		t.Fatal("compactor command returned an empty result")
	}
}

// TestCancelRunningWaitsForLiveRunTeardown covers the live child the process-
// level registry must reap when Bubble Tea returns no model. The fake cake is a
// real child; no real cake binary is needed.
func TestCancelRunningWaitsForLiveRunTeardown(t *testing.T) {
	started := filepath.Join(t.TempDir(), "started")
	t.Setenv("STARTED_FILE", started)
	bin := writeFakeCake(t, `
 touch "$STARTED_FILE"
 trap 'exit 0' TERM
 while :; do sleep 0.05; done
 `)

	children := childproc.NewRegistry()
	m := laidOut(80, 24)
	m.cfg.CakeBin = bin
	m.cfg.Cwd = t.TempDir()
	m.cfg.Children = children
	tm, _ := m.startRun("hi")
	m = tm.(Model)
	if m.run == nil {
		t.Fatal("startRun did not leave a live run")
	}
	t.Cleanup(m.run.Cancel)
	waitForFile(t, started)
	if got := children.Len(); got != 1 {
		t.Fatalf("registry tracks %d children, want the live run", got)
	}

	if err := m.CancelRunning(); err != nil {
		t.Fatalf("CancelRunning: %v", err)
	}
	select {
	case <-m.run.Done:
	default:
		t.Fatal("CancelRunning returned before the live run was reaped")
	}
	if res := <-m.run.Result; !res.Canceled {
		t.Fatalf("live run result = %+v, want Canceled", res)
	}
}

// TestCancelRunningDeadlineReportsTimedOutChildren proves the shutdown bound is
// shared across every operation and that a wedged child is named rather than
// silently abandoned.
func TestCancelRunningDeadlineReportsTimedOutChildren(t *testing.T) {
	never := make(chan struct{})
	m := Model{
		run:             &cake.Run{Done: never},
		replayDone:      never,
		compaction:      pendingOperation{phase: phaseDraining, done: never},
		suggestion:      pendingOperation{phase: phaseDraining, done: never},
		sessionListDone: map[uint64]chan struct{}{7: never},
	}

	start := time.Now()
	err := m.cancelRunning(30 * time.Millisecond)
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("cancelRunning returned nil for children that never reaped")
	}
	if elapsed > time.Second {
		t.Fatalf("cancelRunning took %s, want the 30ms bound", elapsed)
	}
	for _, want := range []string{"run", "replay", "compaction", "suggestion", "session listing 7"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("timeout error %q does not name %q", err, want)
		}
	}
}
