package cake

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"runtime"
	"strings"
	"syscall"
	"time"
)

// RunMode selects which session flag a cake invocation uses.
type RunMode int

const (
	// RunFresh starts a new cake session (no session flag).
	RunFresh RunMode = iota
	// RunContinue passes --continue to use the latest session for the cwd.
	RunContinue
	// RunResume passes --resume <uuid> for a specific session.
	RunResume
)

func (m RunMode) String() string {
	switch m {
	case RunContinue:
		return "continue"
	case RunResume:
		return "resume"
	default:
		return "fresh"
	}
}

// Options configures one cake invocation.
type Options struct {
	Bin      string
	Cwd      string
	Prompt   string
	Mode     RunMode
	ResumeID string
	Model    string
	Profile  string
	DebugLog io.Writer
}

// Args returns the cake CLI arguments for these options.
func (o Options) Args() []string {
	args := []string{"--output-format", "stream-json"}
	switch o.Mode {
	case RunContinue:
		args = append(args, "--continue")
	case RunResume:
		args = append(args, "--resume", o.ResumeID)
	case RunFresh:
	}
	if o.Model != "" {
		args = append(args, "--model", o.Model)
	}
	if o.Profile != "" {
		args = append(args, "--profile", o.Profile)
	}
	// "--" ends flag parsing so a prompt starting with "-" (or matching a
	// cake subcommand name) is always treated as the prompt.
	return append(args, "--", o.Prompt)
}

// Result is the terminal state of one cake invocation.
type Result struct {
	ExitCode int
	Stderr   string
	Canceled bool
	Err      error
}

// ParseError is emitted as a synthetic event when a stdout line is not valid
// JSON. The stream keeps flowing after it.
type ParseError struct {
	Line string
	Err  error
}

func (e ParseError) EventType() string { return "parse_error" }

// Run is one live cake subprocess. Events is closed when stdout reaches EOF;
// Result then delivers exactly one value.
type Run struct {
	Events <-chan Event
	Result <-chan Result
	cancel context.CancelFunc
}

// Cancel asks the cake process to stop (SIGTERM, then SIGKILL after a grace
// period). Safe to call more than once.
func (r *Run) Cancel() {
	if r.cancel != nil {
		r.cancel()
	}
}

// stderrLimit bounds how much captured stderr is kept for error display.
const stderrLimit = 8 * 1024

// Start launches one cake process for the given options. It returns an error
// only when the process cannot be started at all (for example, the binary is
// missing); everything after a successful start is reported through Events
// and Result.
func Start(opts Options) (*Run, error) {
	ctx, cancel := context.WithCancel(context.Background())

	cmd := exec.CommandContext(ctx, opts.Bin, opts.Args()...) // #nosec G204 -- opts.Bin intentionally comes from --cake-bin/config.
	if opts.Cwd != "" {
		cmd.Dir = opts.Cwd
	}
	// Prefer a graceful stop so cake can finish writing session records, but
	// fall back to SIGKILL if it does not exit promptly. Windows cannot
	// deliver SIGTERM (Process.Signal returns an error, which would stall
	// cancellation for the full WaitDelay), so kill outright there.
	cmd.Cancel = func() error {
		if runtime.GOOS == "windows" {
			return cmd.Process.Kill()
		}
		return cmd.Process.Signal(syscall.SIGTERM)
	}
	cmd.WaitDelay = 3 * time.Second

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("opening stdout pipe: %w", err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("starting %s: %w", opts.Bin, err)
	}

	events := make(chan Event, 64)
	result := make(chan Result, 1)

	go func() {
		defer close(events)
		// bufio.Reader instead of Scanner: tool outputs can produce very
		// long single lines and must not abort the stream.
		reader := bufio.NewReader(stdout)
		for {
			line, readErr := reader.ReadString('\n')
			if line != "" {
				if opts.DebugLog != nil {
					fmt.Fprintf(opts.DebugLog, "stdout: %s", line)
				}
				ev, parseErr := ParseLine([]byte(line))
				if parseErr != nil {
					events <- ParseError{Line: snippet(line, 200), Err: parseErr}
				} else if ev != nil {
					events <- ev
				}
			}
			if readErr != nil {
				if opts.DebugLog != nil && !errors.Is(readErr, io.EOF) {
					fmt.Fprintf(opts.DebugLog, "stdout read error: %v\n", readErr)
				}
				break
			}
		}

		waitErr := cmd.Wait()
		res := Result{
			Canceled: ctx.Err() != nil,
			Stderr:   tail(stderr.String(), stderrLimit),
		}
		var exitErr *exec.ExitError
		switch {
		case waitErr == nil:
			res.ExitCode = 0
		case errors.As(waitErr, &exitErr):
			res.ExitCode = exitErr.ExitCode()
		default:
			res.ExitCode = -1
			res.Err = waitErr
		}
		if opts.DebugLog != nil {
			fmt.Fprintf(opts.DebugLog, "exit: code=%d canceled=%v err=%v\n", res.ExitCode, res.Canceled, res.Err)
		}
		result <- res
	}()

	return &Run{Events: events, Result: result, cancel: cancel}, nil
}

// snippet returns at most n characters of s with newlines flattened.
func snippet(s string, n int) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\n", " "))
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}

// tail returns the last n bytes of s, trimmed.
func tail(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) > n {
		return "…" + s[len(s)-n:]
	}
	return s
}
