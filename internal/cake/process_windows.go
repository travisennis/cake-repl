//go:build windows

package cake

import (
	"errors"
	"os"
	"syscall"
)

// signalGroup terminates the process on Windows. There is no process-group
// equivalent, so the second argument is ignored.
func signalGroup(pid int, _ syscall.Signal) error {
	if pid <= 0 {
		return nil
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return p.Kill()
}

// killGroup has no group to target on Windows; it terminates the process.
func killGroup(pid int) error {
	return signalGroup(pid, 0)
}

// processGone reports whether err means the target no longer exists.
func processGone(err error) bool {
	return errors.Is(err, os.ErrProcessDone)
}

// Stop is a no-op on Windows, which has no SIGSTOP.
func (r *Run) Stop() error { return nil }

// Continue is a no-op on Windows, which has no SIGCONT.
func (r *Run) Continue() error { return nil }
