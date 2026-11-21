package app

import (
	"fmt"
	"strings"
	"time"

	"github.com/travisennis/cake-repl/internal/childproc"
)

const ChildTeardownTimeout = 5 * time.Second

func (m Model) trackChild(name string, cancel func(), done <-chan struct{}) {
	if m.cfg.Children != nil {
		m.cfg.Children.Register(name, cancel, done)
	}
}

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
