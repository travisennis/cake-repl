//go:build !windows

package childproc

import (
	"os/exec"
	"syscall"
)

// Isolate puts cmd in its own session with no controlling terminal. The child
// and its descendants cannot open /dev/tty, while ordinary signals to the
// child still work and stdout/stderr stay the captured pipes. Callers must
// treat a start failure after this attribute as fatal: there is no unisolated
// retry.
func Isolate(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
