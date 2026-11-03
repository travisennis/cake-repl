package app

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// childTeardownTimeout bounds the whole unobserved-exit shutdown. The longest
// child path is a live run: SIGTERM, a three-second grace before the group
// SIGKILL, and a one-second-later PID-only WaitDelay fallback. Five seconds
// leaves scheduling room without letting a wedged child hold the REPL open.
const childTeardownTimeout = 5 * time.Second

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
	m.cancelReplay()
	m.cancelCompaction()
	m.cancelSuggestion()
	m.cancelSessionList()

	ops := make(map[string]<-chan struct{})
	if m.run != nil {
		ops["run"] = m.run.Done
	}
	if m.replayRun != nil {
		ops["replay"] = m.replayRun.Done
	} else if m.replayDone != nil {
		// The startup command owns the child until replayStartedMsg reaches
		// Update. Its done channel is the only handle to a replay that is
		// still starting.
		ops["replay"] = m.replayDone
	}
	if m.compactionDone != nil {
		ops["compaction"] = m.compactionDone
	}
	if m.suggestionDone != nil {
		ops["suggestion"] = m.suggestionDone
	}
	for req, done := range m.sessionListDone {
		ops[fmt.Sprintf("session listing %d", req)] = done
	}

	timedOut := waitForReap(ops, timeout)
	if len(timedOut) == 0 {
		return nil
	}
	err := fmt.Errorf("shutdown timed out waiting for %s", strings.Join(timedOut, ", "))
	if m.cfg.DebugLog != nil {
		fmt.Fprintf(m.cfg.DebugLog, "repl: %v\n", err)
	}
	return err
}

// waitForReap waits until every done channel closes or timeout expires. It
// returns the sorted names still pending at the deadline. A nil channel counts
// as already reaped so a zero-value test model cannot turn shutdown into an
// unbounded wait.
func waitForReap(ops map[string]<-chan struct{}, timeout time.Duration) []string {
	if len(ops) == 0 {
		return nil
	}

	finished := make(chan string, len(ops))
	stop := make(chan struct{})
	pending := make(map[string]struct{}, len(ops))
	for name, done := range ops {
		pending[name] = struct{}{}
		if done == nil {
			finished <- name
			continue
		}
		go func(name string, done <-chan struct{}) {
			select {
			case <-done:
				finished <- name
			case <-stop:
			}
		}(name, done)
	}

	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for len(pending) > 0 {
		select {
		case name := <-finished:
			delete(pending, name)
		case <-timer.C:
			close(stop)
			// A child can be reaped as the timer fires. Sweep once so a
			// completed channel is not reported as timed out.
			for name := range pending {
				done := ops[name]
				if done == nil {
					delete(pending, name)
					continue
				}
				select {
				case <-done:
					delete(pending, name)
				default:
				}
			}
			names := make([]string, 0, len(pending))
			for name := range pending {
				names = append(names, name)
			}
			sort.Strings(names)
			return names
		}
	}
	close(stop)
	return nil
}
