//go:build !windows

package cake

import (
	"errors"
	"os/exec"
	"syscall"
)

// configureProcess puts the child in its own session with no controlling
// terminal. A session leader without a controlling terminal cannot open
// /dev/tty, so a child that tries fails with ENXIO instead of changing the
// REPL's terminal modes. See ADR 027.
func configureProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}

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

// processGone reports whether err means the target no longer exists. A group
// signal can race a run that exited between the registry snapshot and the
// signal; that race is accepted, but it must not be logged as a failure.
func processGone(err error) bool {
	return errors.Is(err, syscall.ESRCH)
}
