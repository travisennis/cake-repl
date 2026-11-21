package cake

import (
	"sync"

	"github.com/travisennis/cake-repl/internal/childproc"
)

type Registry struct {
	mu   sync.Mutex
	runs map[*Run]struct{}
	logf func(string, ...any)

	cycleMu   sync.Mutex
	suspended bool
}

func NewRegistry(logf func(string, ...any)) *Registry {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	return &Registry{runs: make(map[*Run]struct{}), logf: logf}
}

func (r *Registry) Add(run *Run) {
	if r == nil || run == nil {
		return
	}
	r.mu.Lock()
	if r.runs == nil {
		r.runs = make(map[*Run]struct{})
	}
	r.runs[run] = struct{}{}
	suspended := r.suspended
	r.mu.Unlock()
	if !suspended {
		return
	}

	r.cycleMu.Lock()
	defer r.cycleMu.Unlock()
	r.mu.Lock()
	suspended = r.suspended
	r.mu.Unlock()
	if suspended {
		r.logFailure("stopping", run, run.Stop())
	}
}

func (r *Registry) Remove(run *Run) {
	if r == nil {
		return
	}
	r.mu.Lock()
	delete(r.runs, run)
	r.mu.Unlock()
}

func (r *Registry) StopAll() {
	if r == nil {
		return
	}
	r.cycleMu.Lock()
	defer r.cycleMu.Unlock()
	r.mu.Lock()
	r.suspended = true
	r.mu.Unlock()
	for _, run := range r.snapshot() {
		r.logFailure("stopping", run, run.Stop())
	}
}

func (r *Registry) ContinueAll() {
	if r == nil {
		return
	}
	r.cycleMu.Lock()
	defer r.cycleMu.Unlock()
	r.mu.Lock()
	r.suspended = false
	r.mu.Unlock()
	for _, run := range r.snapshot() {
		r.logFailure("continuing", run, run.Continue())
	}
}

func (r *Registry) logFailure(action string, run *Run, err error) {
	if err == nil || childproc.ProcessGone(err) || r.logf == nil {
		return
	}
	r.logf("suspend: %s engine run %d failed: %v", action, run.pid.Load(), err)
}

func (r *Registry) snapshot() []*Run {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	runs := make([]*Run, 0, len(r.runs))
	for run := range r.runs {
		runs = append(runs, run)
	}
	return runs
}
