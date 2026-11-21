//go:build !windows

package childproc

import (
	"os/exec"
	"syscall"
)

func Isolate(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
