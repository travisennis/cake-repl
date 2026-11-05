package childproc

import (
	"context"
	"errors"
	"os/exec"
	"runtime"
)

// Outcome is the shared terminal classification of one external child process.
// Each caller maps it into its own result type so package-specific wording and
// document handling stay where they belong.
type Outcome struct {
	// ExitCode is the process exit status, or -1 when none was observed.
	ExitCode int
	// Stderr is the bounded diagnostic tail captured for the child.
	Stderr string
	// Canceled reports that the REPL's cancellation terminated the child. A
	// natural non-zero exit stays a failure even when a cancellation races it.
	Canceled bool
	// TimedOut reports that the operation's context deadline expired. Callers
	// decide the wording: the suggester reports a failure, while a caller that
	// already treats cancellation uniformly may keep its cancellation path.
	TimedOut bool
	// Err is a start or teardown failure, or the context error when a timeout
	// left no process status. A non-zero exit is represented by ExitCode.
	Err error
}

// Classify maps the result of cmd.Run (waitErr) and the operation context's
// error (ctxErr) onto Outcome. cancelRequested says whether the caller's Cancel
// callback successfully signaled the child; it is what separates a
// signal-terminated cancellation from a natural crash.
//
// The precedence matches the engine, compactor, and suggester callers:
//   - a clean exit is success, even if the context expired as it finished;
//   - an ExitError is classified from its status and cancelRequested;
//   - an interrupted wait without a status is a cancellation;
//   - a context deadline sets TimedOut without hiding how the process ended;
//   - anything else is a start or teardown failure.
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

// canceledExit reports whether a non-zero exit is a cancellation rather than a
// natural failure. On POSIX, signaling an exited-but-unreaped process can
// succeed, so both the cancel callback and a signal-terminated status are
// required; Windows has no signal status, so a successful cancel is enough.
func canceledExit(exitErr *exec.ExitError, cancelRequested bool) bool {
	if !cancelRequested {
		return false
	}
	return runtime.GOOS == "windows" || !exitErr.Exited()
}
