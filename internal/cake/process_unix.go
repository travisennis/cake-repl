//go:build !windows

package cake

import (
	"errors"
	"syscall"
)

func signalGroup(pgid int, sig syscall.Signal) error {
	if pgid <= 0 {
		return nil
	}
	return syscall.Kill(-pgid, sig)
}

func killGroup(pid int) error {
	return signalGroup(pid, syscall.SIGKILL)
}

func (r *Run) Stop() error {
	return r.signal(syscall.SIGSTOP)
}

func (r *Run) Continue() error {
	return r.signal(syscall.SIGCONT)
}

func (r *Run) signal(sig syscall.Signal) error {
	if r == nil {
		return nil
	}
	pid := int(r.pid.Load())
	if pid <= 0 || r.currentPhase() == processReaped {
		return nil
	}
	return signalGroup(pid, sig)
}

func processGone(err error) bool {
	return errors.Is(err, syscall.ESRCH)
}
