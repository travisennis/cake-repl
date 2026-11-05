package childproc

import (
	"context"
	"errors"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// runShell runs one short shell command and returns the command with its
// terminal state. The suite already requires a POSIX shell for its fake
// children; these cases exercise the classifier with real process statuses
// rather than hand-built os.ProcessState values.
func runShell(t *testing.T, script string) (*exec.Cmd, error) {
	t.Helper()
	cmd := exec.Command("sh", "-c", script)
	err := cmd.Run()
	return cmd, err
}

func TestClassify(t *testing.T) {
	success, successErr := runShell(t, "exit 0")
	nonzero, nonzeroErr := runShell(t, "exit 3")
	stderrBuf := NewTailBuffer(64)
	withStderr := exec.Command("sh", "-c", "echo boom >&2; exit 1")
	withStderr.Stderr = stderrBuf
	stderrErr := withStderr.Run()
	missing := exec.Command(filepath.Join(t.TempDir(), "does-not-exist"))
	missingErr := missing.Run()

	tests := []struct {
		name            string
		cmd             *exec.Cmd
		waitErr         error
		ctxErr          error
		cancelRequested bool
		stderr          *TailBuffer
		wantExitCode    int
		wantCanceled    bool
		wantTimedOut    bool
		wantErr         bool
		wantStderr      string
	}{
		{
			name:         "success",
			cmd:          success,
			waitErr:      successErr,
			wantExitCode: 0,
		},
		{
			name:         "clean exit wins over an expired context",
			cmd:          success,
			waitErr:      successErr,
			ctxErr:       context.DeadlineExceeded,
			wantExitCode: 0,
		},
		{
			name:         "non-zero exit",
			cmd:          nonzero,
			waitErr:      nonzeroErr,
			wantExitCode: 3,
		},
		{
			name:            "non-zero exit is not canceled by a late request",
			cmd:             nonzero,
			waitErr:         nonzeroErr,
			cancelRequested: true,
			wantExitCode:    3,
			wantCanceled:    runtime.GOOS == "windows",
		},
		{
			name:         "canceled wait without an exit status",
			cmd:          exec.Command("sh", "-c", "exit 0"),
			waitErr:      context.Canceled,
			ctxErr:       context.Canceled,
			wantExitCode: -1,
			wantCanceled: true,
		},
		{
			name:         "deadline keeps the exit status",
			cmd:          nonzero,
			waitErr:      nonzeroErr,
			ctxErr:       context.DeadlineExceeded,
			wantExitCode: 3,
			wantTimedOut: true,
		},
		{
			name:         "deadline without an exit status is an error",
			cmd:          exec.Command("sh", "-c", "exit 0"),
			waitErr:      context.DeadlineExceeded,
			ctxErr:       context.DeadlineExceeded,
			wantExitCode: -1,
			wantTimedOut: true,
			wantErr:      true,
		},
		{
			name:         "start failure",
			cmd:          missing,
			waitErr:      missingErr,
			wantExitCode: -1,
			wantErr:      true,
		},
		{
			name:         "stderr is carried into the outcome",
			cmd:          withStderr,
			waitErr:      stderrErr,
			stderr:       stderrBuf,
			wantExitCode: 1,
			wantStderr:   "boom",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Classify(tt.cmd, tt.waitErr, tt.ctxErr, tt.cancelRequested, tt.stderr)
			if got.ExitCode != tt.wantExitCode {
				t.Errorf("ExitCode = %d, want %d", got.ExitCode, tt.wantExitCode)
			}
			if got.Canceled != tt.wantCanceled {
				t.Errorf("Canceled = %v, want %v", got.Canceled, tt.wantCanceled)
			}
			if got.TimedOut != tt.wantTimedOut {
				t.Errorf("TimedOut = %v, want %v", got.TimedOut, tt.wantTimedOut)
			}
			if (got.Err != nil) != tt.wantErr {
				t.Errorf("Err = %v, wantErr %v", got.Err, tt.wantErr)
			}
			if got.Stderr != tt.wantStderr {
				t.Errorf("Stderr = %q, want %q", got.Stderr, tt.wantStderr)
			}
		})
	}
}

func TestClassifyCanceledSignal(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows cancellation uses the cmd.Cancel flag and has no signal status")
	}
	cmd, err := runShell(t, "kill -TERM $$")
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("Run error = %v, want *exec.ExitError", err)
	}
	got := Classify(cmd, err, nil, true, nil)
	if !got.Canceled || got.ExitCode != -1 || got.Err != nil {
		t.Fatalf("Classify = %+v, want a silent cancellation", got)
	}
	if got := Classify(cmd, err, nil, false, nil); got.Canceled {
		t.Fatalf("Classify = %+v, want an unsignaled signal exit to stay a failure", got)
	}
}
