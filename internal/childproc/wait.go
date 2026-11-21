package childproc

import (
	"sort"
	"time"
)

type Operation struct {
	Name string
	Done <-chan struct{}
}

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
