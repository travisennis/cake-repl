package app

import (
	"fmt"
	"strings"
	"time"

	"github.com/travisennis/cake-repl/internal/childproc"
)

// ChildTeardownTimeout bounds the whole unobserved-exit shutdown. The longest
// child path is a live run: SIGTERM, a three-second grace before the group
// SIGKILL, and a one-second-later PID-only WaitDelay fallback. Five seconds
// leaves scheduling room without letting a wedged child hold the REPL open.
const ChildTeardownTimeout = 5 * time.Second

// trackChild registers one child operation with the process-level registry main
// owns. Without a registry, the model-owned handles remain the shutdown path.
func (m Model) trackChild(name string, cancel func(), done <-chan struct{}) {
	if m.cfg.Children != nil {
		m.cfg.Children.Register(name, cancel, done)
	}
}

// cancelRunning cancels every child operation and waits for all of them to be
// reaped by one shared deadline. It waits on reaping channels rather than result
// messages, because the Bubble Tea Update loop that normally consumes results
// may already be gone. A child that misses the deadline is named in the returned
// error; it remains observed by its own runner goroutine rather than being
// silently abandoned.
func (m Model) cancelRunning(timeout time.Duration) error {
	if m.run != nil {
		m.run.Cancel()
	}
	m.cancelRun()
	m.cancelReplay()
	m.cancelCompaction()
	m.cancelSuggestion()
	m.cancelSessionList()

	ops := make([]childproc.Operation, 0, 5+len(m.sessionListDone))
	if done := m.runOp.doneChannel(); done != nil {
		ops = append(ops, childproc.Operation{Name: "run", Done: done})
	} else if m.run != nil {
		ops = append(ops, childproc.Operation{Name: "run", Done: m.run.Done})
	}
	if done := m.replay.doneChannel(); done != nil {
		// The same reaping channel covers a startup command that has not
		// delivered replayStartedMsg and a running replay whose result has not
		// settled yet.
		ops = append(ops, childproc.Operation{Name: "replay", Done: done})
	}
	if done := m.compaction.doneChannel(); done != nil {
		ops = append(ops, childproc.Operation{Name: "compaction", Done: done})
	}
	if done := m.suggestion.doneChannel(); done != nil {
		ops = append(ops, childproc.Operation{Name: "suggestion", Done: done})
	}
	for req, done := range m.sessionListDone {
		ops = append(ops, childproc.Operation{Name: fmt.Sprintf("session listing %d", req), Done: done})
	}

	timedOut := childproc.WaitForReap(ops, timeout)
	if len(timedOut) == 0 {
		return nil
	}
	return fmt.Errorf("shutdown timed out waiting for %s", strings.Join(timedOut, ", "))
}
