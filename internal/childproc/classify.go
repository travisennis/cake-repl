package childproc

import (
	"context"
	"errors"
	"os/exec"
	"runtime"
)

type Outcome struct {
	ExitCode int
	Stderr   string
	Canceled bool
	TimedOut bool
	Err      error
}

func Classify(cmd *exec.Cmd, waitErr, ctxErr error, cancelRequested bool, stderr *TailBuffer) Outcome {
	out := Outcome{ExitCode: -1}
	if stderr != nil {
		out.Stderr = stderr.String()
	}
	if waitErr == nil {
		out.ExitCode = 0
		return out
	}
	out.TimedOut = errors.Is(ctxErr, context.DeadlineExceeded) ||
		errors.Is(waitErr, context.DeadlineExceeded)

	var exitErr *exec.ExitError
	switch {
	case errors.As(waitErr, &exitErr):
		out.ExitCode = exitErr.ExitCode()
		out.Canceled = canceledExit(exitErr, cancelRequested)
	case errors.Is(waitErr, context.Canceled):
		out.ExitCode = exitCode(cmd)
		out.Canceled = true
	case out.TimedOut:
		out.ExitCode = exitCode(cmd)
		out.Err = ctxErr
		if out.Err == nil {
			out.Err = waitErr
		}
	default:
		out.Err = waitErr
	}
	return out
}

func exitCode(cmd *exec.Cmd) int {
	if cmd.ProcessState == nil {
		return -1
	}
	return cmd.ProcessState.ExitCode()
}

func canceledExit(exitErr *exec.ExitError, cancelRequested bool) bool {
	if !cancelRequested {
		return false
	}
	return runtime.GOOS == "windows" || !exitErr.Exited()
}
