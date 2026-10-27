//go:build !windows

package cake

import (
	"errors"
	"syscall"
)

// signalGroup sends sig to the process group led by pgid. A non-positive id is
// a no-op: pid 0 would target the REPL's own group.
func signalGroup(pgid int, sig syscall.Signal) error {
	if pgid <= 0 {
		return nil
	}
	return syscall.Kill(-pgid, sig)
}

// killGroup kills the engine's whole process group. The run is detached into
// its own session, so the leader's pid is also the group id.
func killGroup(pid int) error {
	return signalGroup(pid, syscall.SIGKILL)
}

// Stop pauses the run for a REPL suspend. A finished run is not signaled.
func (r *Run) Stop() error {
	return r.signal(syscall.SIGSTOP)
}

// Continue resumes a run paused by Stop. A finished run is not signaled.
func (r *Run) Continue() error {
	return r.signal(syscall.SIGCONT)
}

func (r *Run) signal(sig syscall.Signal) error {
	if r == nil || r.pid <= 0 || r.exited.Load() {
		return nil
	}
	return signalGroup(r.pid, sig)
}

// processGone reports whether err means the target no longer exists. A group
// signal can race a run that exited between the registry snapshot and the
// signal; that race is accepted, but it must not be logged as a failure.
func processGone(err error) bool {
	return errors.Is(err, syscall.ESRCH)
}
