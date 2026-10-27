//go:build !windows

package cake

import (
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
