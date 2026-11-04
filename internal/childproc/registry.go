package childproc

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// Registry tracks cancellable child operations for the process-level shutdown
// path. main owns the registry and passes it to the app; each operation
// registers itself as it starts, so shutdown can cancel and reap it even when
// Bubble Tea returns no model after an Update or View panic. A nil Registry is
// safe and simply leaves operations untracked.
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

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{ops: make(map[*operation]struct{})}
}

// Register adds one child operation. A nil registry or nil done channel leaves
// the operation untracked. cancel may be nil. Once Shutdown has begun, Register
// cancels the new operation immediately and leaves it untracked instead of
// adding it to the wait set.
func (r *Registry) Register(name string, cancel func(), done <-chan struct{}) {
	if r == nil || done == nil {
		return
	}

	// A completed operation is not worth tracking; this also keeps a fast
	// child from leaving a removal goroutine behind.
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

// Len reports how many operations are still tracked. A completed operation is
// removed by its registration goroutine, so Len can briefly include a child
// whose done channel has already closed.
func (r *Registry) Len() int {
	if r == nil {
		return 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.ops)
}

// Shutdown cancels every tracked operation and waits for all of their done
// channels under one deadline. It returns an error naming the operations that
// missed the deadline. Once Shutdown begins, later registrations are canceled
// immediately rather than admitted. A nil registry is a no-op.
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
