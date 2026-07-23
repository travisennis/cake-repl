package cake

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"runtime"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
	"unicode/utf8"
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

	afterWait func()
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

// stderrLimit bounds how much stderr is retained for error display; older
// output is dropped as it arrives so a chatty process cannot grow memory
// without bound.
const stderrLimit = 8 * 1024

// tailBuffer is an io.Writer that keeps only the last limit bytes written.
type tailBuffer struct {
	limit     int
	buf       []byte
	truncated bool
}

func (b *tailBuffer) Write(p []byte) (int, error) {
	n := len(p)
	if n > b.limit {
		b.truncated = true
		p = p[n-b.limit:]
	}
	b.buf = append(b.buf, p...)
	if overflow := len(b.buf) - b.limit; overflow > 0 {
		b.truncated = true
		copy(b.buf, b.buf[overflow:])
		b.buf = b.buf[:b.limit]
	}
	return n, nil
}

// String returns the retained tail, trimmed, with an ellipsis marking any
// dropped output.
func (b *tailBuffer) String() string {
	buf := b.buf
	if b.truncated {
		// The byte cut can land mid-rune; drop continuation bytes so the
		// kept tail starts on a rune boundary.
		for len(buf) > 0 && !utf8.RuneStart(buf[0]) {
			buf = buf[1:]
		}
	}
	s := strings.TrimSpace(string(buf))
	if b.truncated {
		return "…" + s
	}
	return s
}

// Start launches one cake process for the given options. It returns an error
// only when the process cannot be started at all (for example, the binary is
// missing); everything after a successful start is reported through Events
// and Result.
func Start(opts Options) (*Run, error) {
	ctx, cancel := context.WithCancel(context.Background())

	cmd := exec.CommandContext(ctx, opts.Bin, opts.Args()...) // #nosec G204 -- opts.Bin intentionally comes from -cake-bin/config.
	if opts.Cwd != "" {
		cmd.Dir = opts.Cwd
	}
	// Prefer a graceful stop so cake can finish writing session records, but
	// fall back to SIGKILL if it does not exit promptly. Windows cannot
	// deliver SIGTERM (Process.Signal returns an error, which would stall
	// cancellation for the full WaitDelay), so kill outright there.

	// cancelRequested records whether the exec context watcher successfully
	// signaled the process. Unlike ctx.Err() it stays false for a cancel after
	// Wait, and canceledExit checks the final POSIX status to disambiguate an
	// exited-but-unreaped process.
	var cancelRequested atomic.Bool

	cmd.Cancel = func() error {
		// Only record a successful signal. If the process has already
		// exited, os/exec treats os.ErrProcessDone as a natural completion
		// and Wait synchronizes with this callback before returning.
		var err error
		if runtime.GOOS == "windows" {
			err = cmd.Process.Kill()
		} else {
			err = cmd.Process.Signal(syscall.SIGTERM)
		}
		if err == nil {
			cancelRequested.Store(true)
		}
		return err
	}
	cmd.WaitDelay = 3 * time.Second

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("opening stdout pipe: %w", err)
	}
	stderr := &tailBuffer{limit: stderrLimit}
	cmd.Stderr = stderr

	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("starting %s: %w", opts.Bin, err)
	}

	events := make(chan Event, 64)
	result := make(chan Result, 1)

	go func() {
		// Release the context once the run is over; Run.Cancel is the only
		// other caller and a normal completion never invokes it.
		defer cancel()
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
		if opts.afterWait != nil {
			opts.afterWait()
		}
		// Classify cancellation from whether cmd.Cancel was invoked while
		// Wait was active, not from ctx directly: a Ctrl+C landing after
		// Wait has returned must not relabel a finished run as canceled.
		res := Result{Stderr: stderr.String()}
		var exitErr *exec.ExitError
		switch {
		case waitErr == nil:
			res.ExitCode = 0
		case errors.Is(waitErr, context.Canceled):
			// The cancel interrupted the command and it then exited cleanly
			// (cake handled SIGTERM); exec reports ctx.Err() in that case.
			res.Canceled = true
			res.ExitCode = cmd.ProcessState.ExitCode()
		case errors.As(waitErr, &exitErr):
			res.ExitCode = exitErr.ExitCode()
			res.Canceled = canceledExit(exitErr, cancelRequested.Load())
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

func canceledExit(exitErr *exec.ExitError, cancelRequested bool) bool {
	if !cancelRequested {
		return false
	}
	// On POSIX, signaling an exited-but-unreaped process can succeed. Require
	// signal termination as well as the cmd.Cancel flag so that natural
	// non-zero exits stay failures. Windows Kill does not expose that state.
	return runtime.GOOS == "windows" || !exitErr.Exited()
}

// snippet returns at most n bytes of s with newlines flattened, never
// splitting a multibyte rune.
func snippet(s string, n int) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\n", " "))
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n] + "…"
}
