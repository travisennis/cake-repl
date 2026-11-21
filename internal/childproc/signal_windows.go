//go:build windows

package childproc

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"

	"golang.org/x/sys/windows"
)

func signalTerm(proc *os.Process) error {
	return proc.Kill()
}

func killGroup(pid int) error {
	if pid <= 0 {
		return nil
	}
	cmd := exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(pid))
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: windows.CREATE_NO_WINDOW,
	}
	out, err := cmd.CombinedOutput()
	if err == nil {
		return nil
	}
	msg := strings.ToLower(string(out))
	if strings.Contains(msg, "not found") || strings.Contains(msg, "no running instance") {
		return nil
	}
	return fmt.Errorf("taskkill: %w: %s", err, strings.TrimSpace(string(out)))
}

func processGone(err error) bool {
	return errors.Is(err, os.ErrProcessDone)
}
