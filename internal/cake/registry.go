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

	// cycleMu serializes a stop/continue cycle with Add, so a run registered
	// after StopAll's snapshot is still paused. suspended is guarded by mu.
	cycleMu   sync.Mutex
	suspended bool
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
	suspended := r.suspended
	r.mu.Unlock()
	if !suspended {
		return
	}

	// A run can start between StopAll's snapshot and the REPL's self-stop.
	// Serialize with the cycle and re-check: if a resume won the race, the run
	// must not be stopped after ContinueAll already ran.
	r.cycleMu.Lock()
	defer r.cycleMu.Unlock()
	r.mu.Lock()
	suspended = r.suspended
	r.mu.Unlock()
	if suspended {
		r.logFailure("stopping", run, run.Stop())
	}
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

// ContinueAll resumes every registered run's process group. A run that exited
// during the suspend is skipped by Run.Continue, and an ESRCH from a race is
// not logged.
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
	if err == nil || processGone(err) || r.logf == nil {
		return
	}
	r.logf("suspend: %s engine run %d failed: %v", action, run.pid, err)
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
