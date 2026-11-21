//go:build !windows

package childproc

import (
	"errors"
	"os"
	"syscall"
)

func signalTerm(proc *os.Process) error {
	return proc.Signal(syscall.SIGTERM)
}

func killGroup(pid int) error {
	if pid <= 0 {
		return nil
	}
	return syscall.Kill(-pid, syscall.SIGKILL)
}

func processGone(err error) bool {
	return errors.Is(err, syscall.ESRCH)
}
