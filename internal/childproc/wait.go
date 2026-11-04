package childproc

import (
	"sort"
	"time"
)

// Operation is one named child operation and the channel that closes when its
// child has been reaped. Names are for reporting only; two operations may share
// one without collapsing into a single wait.
type Operation struct {
	Name string
	Done <-chan struct{}
}

// WaitForReap waits until every done channel closes or timeout expires. It
// returns the sorted, de-duplicated names still pending at the deadline. A nil
// channel counts as already reaped so a zero-value operation cannot turn
// shutdown into an unbounded wait.
func WaitForReap(ops []Operation, timeout time.Duration) []string {
	if len(ops) == 0 {
		return nil
	}

	finished := make(chan int, len(ops))
	stop := make(chan struct{})
	pending := make(map[int]struct{}, len(ops))
	for i, op := range ops {
		pending[i] = struct{}{}
		if op.Done == nil {
			finished <- i
			continue
		}
		go func(i int, done <-chan struct{}) {
			select {
			case <-done:
				finished <- i
			case <-stop:
			}
		}(i, op.Done)
	}

	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for len(pending) > 0 {
		select {
		case i := <-finished:
			delete(pending, i)
		case <-timer.C:
			close(stop)
			// A child can be reaped as the timer fires. Sweep once so a
			// completed channel is not reported as timed out.
			for i := range pending {
				done := ops[i].Done
				if done == nil {
					delete(pending, i)
					continue
				}
				select {
				case <-done:
					delete(pending, i)
				default:
				}
			}
			names := make([]string, 0, len(pending))
			seen := make(map[string]struct{}, len(pending))
			for i := range pending {
				name := ops[i].Name
				if _, ok := seen[name]; ok {
					continue
				}
				seen[name] = struct{}{}
				names = append(names, name)
			}
			sort.Strings(names)
			return names
		}
	}
	close(stop)
	return nil
}
