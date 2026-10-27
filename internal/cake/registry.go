package cake

import "sync"

// Registry tracks the engine runs that must be paused with the REPL around an
// external suspend. A run registers after a successful start and removes itself
// when cmd.Wait returns, so the set follows process lifetime rather than model
// state. StopAll and ContinueAll snapshot the set and signal outside the lock;
// a run that exits between the snapshot and the signal is tolerated.
type Registry struct {
	mu   sync.Mutex
	runs map[*Run]struct{}
	logf func(string, ...any)
}

// NewRegistry returns an empty registry. A nil logf discards diagnostics.
func NewRegistry(logf func(string, ...any)) *Registry {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	return &Registry{runs: make(map[*Run]struct{}), logf: logf}
}

// Add registers a run. It is safe on a nil registry and ignores a nil run.
func (r *Registry) Add(run *Run) {
	if r == nil || run == nil {
		return
	}
	r.mu.Lock()
	if r.runs == nil {
		r.runs = make(map[*Run]struct{})
	}
	r.runs[run] = struct{}{}
	r.mu.Unlock()
}

// Remove unregisters a run. It is safe on a nil registry.
func (r *Registry) Remove(run *Run) {
	if r == nil {
		return
	}
	r.mu.Lock()
	delete(r.runs, run)
	r.mu.Unlock()
}

// StopAll pauses every registered run's process group. The mutex is released
// before any signal, so a slow syscall cannot block registration or removal.
func (r *Registry) StopAll() {
	for _, run := range r.snapshot() {
		if err := run.Stop(); err != nil && !processGone(err) && r.logf != nil {
			r.logf("suspend: stopping engine run %d failed: %v", run.pid, err)
		}
	}
}

// ContinueAll resumes every registered run's process group. A run that exited
// during the suspend is skipped by Run.Continue, and an ESRCH from a race is
// not logged.
func (r *Registry) ContinueAll() {
	for _, run := range r.snapshot() {
		if err := run.Continue(); err != nil && !processGone(err) && r.logf != nil {
			r.logf("suspend: continuing engine run %d failed: %v", run.pid, err)
		}
	}
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
