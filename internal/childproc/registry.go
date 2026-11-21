package childproc

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

type Registry struct {
	mu     sync.Mutex
	ops    map[*operation]struct{}
	closed bool
}

type operation struct {
	name   string
	cancel func()
	done   <-chan struct{}
}

func NewRegistry() *Registry {
	return &Registry{ops: make(map[*operation]struct{})}
}

func (r *Registry) Register(name string, cancel func(), done <-chan struct{}) {
	if r == nil || done == nil {
		return
	}

	select {
	case <-done:
		return
	default:
	}

	op := &operation{name: name, cancel: cancel, done: done}
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		if cancel != nil {
			cancel()
		}
		return
	}
	r.ops[op] = struct{}{}
	r.mu.Unlock()

	go func() {
		<-done
		r.remove(op)
	}()
}

func (r *Registry) remove(op *operation) {
	if r == nil {
		return
	}
	r.mu.Lock()
	delete(r.ops, op)
	r.mu.Unlock()
}

func (r *Registry) Len() int {
	if r == nil {
		return 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.ops)
}

func (r *Registry) Shutdown(timeout time.Duration) error {
	if r == nil {
		return nil
	}

	r.mu.Lock()
	r.closed = true
	ops := make([]*operation, 0, len(r.ops))
	for op := range r.ops {
		ops = append(ops, op)
	}
	r.mu.Unlock()

	waiting := make([]Operation, 0, len(ops))
	for _, op := range ops {
		if op.cancel != nil {
			op.cancel()
		}
		waiting = append(waiting, Operation{Name: op.name, Done: op.done})
	}

	timedOut := WaitForReap(waiting, timeout)
	if len(timedOut) == 0 {
		return nil
	}
	return fmt.Errorf("shutdown timed out waiting for %s", strings.Join(timedOut, ", "))
}
