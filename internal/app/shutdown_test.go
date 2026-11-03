package app

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/travisennis/cake-repl/internal/cake"
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
	m, cmd := m.startCompaction()
	if cmd == nil || m.compactionDone == nil {
		t.Fatal("startCompaction did not leave a bounded child")
	}
	result := make(chan tea.Msg, 1)
	go func() { result <- cmd() }()
	waitForFile(t, started)

	if err := m.CancelRunning(); err != nil {
		t.Fatalf("CancelRunning: %v", err)
	}
	select {
	case <-m.compactionDone:
	default:
		t.Fatal("CancelRunning returned before the compactor was reaped")
	}
	if msg := <-result; msg == nil {
		t.Fatal("compactor command returned nil")
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
		compactionDone:  never,
		suggestionDone:  never,
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
