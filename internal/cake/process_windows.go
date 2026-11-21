//go:build windows

package cake

import (
	"errors"
	"os"
	"syscall"
)

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

func killGroup(pid int) error {
	return signalGroup(pid, 0)
}

func processGone(err error) bool {
	return errors.Is(err, os.ErrProcessDone)
}

func (r *Run) Stop() error { return nil }

func (r *Run) Continue() error { return nil }
