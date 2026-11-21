//go:build windows

package childproc

import "os/exec"

func Isolate(_ *exec.Cmd) {}
