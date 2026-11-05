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
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/travisennis/cake-repl/internal/childproc"
)

// RunMode selects which session flag a cake invocation uses.
type RunMode int

const (
	// RunFresh starts a new cake session (no session flag).
	RunFresh RunMode = iota
	// RunResume passes --resume <uuid> for a specific session.
	RunResume
)

func (m RunMode) String() string {
	if m == RunResume {
		return "resume"
	}
	return "fresh"
}

// Options configures one cake invocation.
type Options struct {
	Bin          string
	Cwd          string
	Prompt       string
	Mode         RunMode
	ResumeID     string
	Fork         bool
	ForkID       string
	NoSession    bool
	Model        string
	Profile      string
	Tools        string
	NoTools      bool
	ToolboxDirs  []string
	Sandbox      string
	NoSkills     bool
	Skills       string
	SystemPrompt string
	AddDirs      []string
	DebugLog     io.Writer
	// Registry tracks this run so a suspend can stop and continue its process
	// group. Nil means the run is not tracked.
	Registry *Registry

	afterWait func()
	// afterStart is a test hook invoked after cmd.Start succeeds and before the
	// pid is published, so tests can force a cancellation into that window.
	afterStart func(*Run)
	replay     bool
	// waitDelay overrides the grace window between Cancel's SIGTERM and the
	// REPL's process-group SIGKILL; zero keeps the production default (see
	// Start). cmd.WaitDelay is set one second later as a PID-only fallback.
	waitDelay time.Duration
}

// ReplayOptions configures a read-only replay of an existing cake session.
type ReplayOptions struct {
	Bin       string
	Cwd       string
	SessionID string
	DebugLog  io.Writer
	Registry  *Registry
	// Context bounds the replay start and process lifetime. Nil uses
	// context.Background(); the REPL owns a cancellable context so an
	// unobserved exit can stop a replay that is still starting.
	Context context.Context
}

// Replay launches cake's supported stream-json session replay command. Replay
// uses the same event parser and subprocess lifecycle as a live invocation but
// never supplies a prompt or starts a new task.
func Replay(opts ReplayOptions) (*Run, error) {
	ctx := opts.Context
	if ctx == nil {
		ctx = context.Background()
	}
	return start(ctx, Options{
		Bin:      opts.Bin,
		Cwd:      opts.Cwd,
		ResumeID: opts.SessionID,
		DebugLog: opts.DebugLog,
		Registry: opts.Registry,
		replay:   true,
	})
}

// Args returns the cake CLI arguments for these options.
func (o Options) Args() []string {
	if o.replay {
		return []string{"--output-format", "stream-json", "replay", o.ResumeID}
	}

	args := []string{"--output-format", "stream-json"}
	switch o.Mode {
	case RunResume:
		args = append(args, "--resume", o.ResumeID)
	case RunFresh:
		// Fork only the initial fresh invocation. Once cake reports the new
		// session ID, subsequent prompts are pinned to it with --resume.
		if o.Fork {
			args = append(args, "--fork")
			if o.ForkID != "" {
				args = append(args, o.ForkID)
			}
		}
	}
	if o.NoSession {
		args = append(args, "--no-session")
	}
	if o.Model != "" {
		args = append(args, "--model", o.Model)
	}
	if o.Profile != "" {
		args = append(args, "--profile", o.Profile)
	}
	// --add-dir grants cake read-only sandbox access to extra directories;
	// cake resolves relative paths against its own working directory (Cwd).
	for _, dir := range o.AddDirs {
		args = append(args, "--add-dir", dir)
	}
	for _, dir := range o.ToolboxDirs {
		args = append(args, "--toolbox", dir)
	}
	if o.Sandbox != "" {
		args = append(args, "--sandbox", o.Sandbox)
	}
	// --tools restricts the session to a comma-separated list of registered
	// tool names, replacing the config/profile tool selection for this run.
	// The names are passed through opaquely; cake owns the registry.
	if o.Tools != "" {
		args = append(args, "--tools", o.Tools)
	}
	if o.NoTools {
		args = append(args, "--no-tools")
	}
	if o.NoSkills {
		args = append(args, "--no-skills")
	}
	if o.Skills != "" {
		args = append(args, "--skills", o.Skills)
	}
	if o.SystemPrompt != "" {
		args = append(args, "--system-prompt", o.SystemPrompt)
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

// processPhase is the lifecycle of one engine process from launch through
// cmd.Wait. It is separate from Result: the phase says which cancellation
// signal is outstanding, while Result says how the process ended. The phase
// advances only forward, and processReaped is terminal.
type processPhase uint32

const (
	// processStarting is a Run whose cmd.Start has not returned, or has just
	// returned before pid is published; pid may still be unset. A context
	// cancellation can win this race and move directly to
	// processCancelRequested.
	processStarting processPhase = iota
	// processRunning is a successfully started process with no cancel signal
	// sent.
	processRunning
	// processCancelRequested means cmd.Cancel successfully signaled the
	// process. The signal is SIGTERM on POSIX and Kill on Windows.
	processCancelRequested
	// processGraceArmed has a REPL-owned timer that will send SIGKILL to the
	// process group if cmd.Wait has not reaped it by the grace deadline.
	processGraceArmed
	// processGroupKilled means the grace timer fired and the group SIGKILL was
	// attempted. The process may not have been reaped yet.
	processGroupKilled
	// processReaped is terminal: cmd.Wait returned, so no group signal may be
	// sent again.
	processReaped
)

// cancelRequested reports whether cmd.Cancel successfully signaled the process.
// Classification reads this before markReaped replaces the phase; processReaped
// intentionally does not retain cancellation history.
func (p processPhase) cancelRequested() bool {
	switch p {
	case processCancelRequested, processGraceArmed, processGroupKilled:
		return true
	default:
		return false
	}
}

// Run is one cake subprocess. Events is closed when stdout reaches EOF;
// Result then delivers exactly one value. Done is closed when the child has
// been reaped, independently of whether Result has been consumed, so shutdown
// can wait for process teardown without stealing the result.
type Run struct {
	Events <-chan Event
	Result <-chan Result
	Done   <-chan struct{}
	cancel context.CancelFunc
	// pid is the engine's process id and, after Setsid, its process-group id.
	// It is atomic because cmd.Cancel can read it while the start goroutine
	// publishes it after cmd.Start. phase is atomic because cmd.Cancel, the
	// grace timer, and the Wait goroutine advance it on different goroutines.
	// Every group signal checks the terminal phase first so a reaped run is not
	// signaled again.
	pid   atomic.Int64
	phase atomic.Uint32

	mu         sync.Mutex
	graceTimer *time.Timer
}

// currentPhase returns the run's process phase.
func (r *Run) currentPhase() processPhase {
	return processPhase(r.phase.Load())
}

// markRunning records a successful start unless cancellation already won the
// race with cmd.Start.
func (r *Run) markRunning() {
	if r == nil {
		return
	}
	r.phase.CompareAndSwap(uint32(processStarting), uint32(processRunning))
}

// beginCancel records that cmd.Cancel successfully signaled the process and
// reports whether this call owns the cancellation transition. A duplicate
// callback or a cancellation after reaping does not re-arm the grace timer.
func (r *Run) beginCancel() bool {
	if r == nil {
		return false
	}
	for {
		switch phase := r.currentPhase(); phase {
		case processStarting, processRunning:
			if r.phase.CompareAndSwap(uint32(phase), uint32(processCancelRequested)) {
				return true
			}
		default:
			return false
		}
	}
}

// armGrace moves a signaled run to the grace-armed phase. It reports whether
// this call owns the timer, so a reaped or already-escalated run cannot arm one.
func (r *Run) armGrace() bool {
	if r == nil {
		return false
	}
	return r.phase.CompareAndSwap(uint32(processCancelRequested), uint32(processGraceArmed))
}

// markGroupKilled records that the grace timer fired and attempted the group
// kill. It reports whether this timer callback still owns the escalation.
func (r *Run) markGroupKilled() bool {
	if r == nil {
		return false
	}
	return r.phase.CompareAndSwap(uint32(processGraceArmed), uint32(processGroupKilled))
}

// markReaped records that cmd.Wait has reaped the process and stops the grace
// timer. processReaped is terminal, so a later signal is a no-op.
func (r *Run) markReaped() {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.phase.Store(uint32(processReaped))
	if r.graceTimer != nil {
		r.graceTimer.Stop()
	}
	r.mu.Unlock()
}

// Cancel asks the cake process to stop (SIGTERM, then SIGKILL after a grace
// period). Safe to call more than once.
func (r *Run) Cancel() {
	if r.cancel != nil {
		r.cancel()
	}
}

// armGroupKill starts the REPL-owned grace timer. cmd.WaitDelay is a later,
// PID-only fallback, so this timer is the primary bound for a leader that
// ignores SIGTERM and for a descendant that still holds the stdout pipe.
func (r *Run) armGroupKill(delay time.Duration, logf io.Writer) {
	if r == nil {
		return
	}
	pid := int(r.pid.Load())
	if pid <= 0 || !r.armGrace() {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.currentPhase() == processReaped {
		return
	}
	r.graceTimer = time.AfterFunc(delay, func() {
		if !r.markGroupKilled() {
			return
		}
		if err := killGroup(pid); err != nil && !processGone(err) && logf != nil {
			fmt.Fprintf(logf, "cancel: killing process group %d failed: %v\n", pid, err)
		}
	})
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
	return start(context.Background(), opts)
}

// start launches one cake process under parent. Replay supplies a model-owned
// context; a live run uses context.Background and is stopped through Run.Cancel.
func start(parent context.Context, opts Options) (*Run, error) {
	ctx, cancel := context.WithCancel(parent)

	cmd := exec.CommandContext(ctx, opts.Bin, opts.Args()...) // #nosec G204 -- opts.Bin intentionally comes from -cake-bin/config.
	if opts.Cwd != "" {
		cmd.Dir = opts.Cwd
	}
	childproc.Isolate(cmd)
	// Prefer a graceful stop so cake can finish writing session records, but
	// fall back to SIGKILL if it does not exit promptly. Windows cannot
	// deliver SIGTERM (Process.Signal returns an error, which would stall
	// cancellation for the full WaitDelay), so kill outright there.

	// grace is the window between SIGTERM and the REPL-owned group SIGKILL.
	// cmd.WaitDelay is deliberately one second later: its kill is PID-only and
	// can race the leader's reaping, while the group kill is the primary bound.
	grace := opts.waitDelay
	if grace <= 0 {
		grace = 3 * time.Second
	}
	cmd.WaitDelay = grace + time.Second

	events := make(chan Event, 64)
	result := make(chan Result, 1)
	done := make(chan struct{})
	run := &Run{Events: events, Result: result, Done: done, cancel: cancel}

	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return errors.New("process has not started")
		}
		// Only record a successful signal. If the process has already
		// exited, os/exec treats os.ErrProcessDone as a natural completion
		// and Wait synchronizes with this callback before returning. A cancel
		// after Wait therefore never reaches here, so it cannot relabel a
		// finished run as canceled.
		var err error
		if runtime.GOOS == "windows" {
			err = cmd.Process.Kill()
		} else {
			err = cmd.Process.Signal(syscall.SIGTERM)
		}
		if err != nil {
			return err
		}
		if run.beginCancel() && runtime.GOOS != "windows" {
			run.armGroupKill(grace, opts.DebugLog)
		}
		return nil
	}

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
	if opts.afterStart != nil {
		opts.afterStart(run)
	}
	run.pid.Store(int64(cmd.Process.Pid))
	run.markRunning()
	// A context cancellation can reach cmd.Cancel before the pid is published.
	// armGroupKill then returns without a timer; re-arm here once the pid is
	// visible. armGrace's CAS makes the two callers single-shot.
	if run.currentPhase() == processCancelRequested {
		run.armGroupKill(grace, opts.DebugLog)
	}
	if opts.Registry != nil {
		opts.Registry.Add(run)
	}

	go func() {
		// Release the context once the run is over; Run.Cancel is the only
		// other caller and a normal completion never invokes it.
		defer cancel()
		defer close(events)
		// bufio.Reader instead of Scanner: tool outputs can produce very
		// long single lines and must not abort the stream.
		reader := bufio.NewReader(stdout)
		// A dead Update loop stops consuming Events. Once cancellation is
		// requested, drop further events and keep draining stdout so the
		// process can exit and be reaped instead of blocking on a full channel.
		dropping := false
		sendEvent := func(ev Event) {
			if dropping {
				return
			}
			// Preserve the normal event stream while the consumer keeps up.
			// Only a blocked send after cancellation is dropped, which is the
			// unobserved-exit case where no Update loop will drain the channel.
			select {
			case events <- ev:
				return
			default:
			}
			select {
			case events <- ev:
			case <-ctx.Done():
				dropping = true
			}
		}
		for {
			line, readErr := reader.ReadString('\n')
			if line != "" {
				if opts.DebugLog != nil {
					fmt.Fprintf(opts.DebugLog, "stdout: %s", line)
				}
				ev, parseErr := ParseLine([]byte(line))
				if parseErr != nil {
					sendEvent(ParseError{Line: snippet(line, 200), Err: parseErr})
				} else if ev != nil {
					sendEvent(ev)
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
		cancelRequested := run.currentPhase().cancelRequested()
		run.markReaped()
		close(done)
		if opts.Registry != nil {
			opts.Registry.Remove(run)
		}
		if opts.afterWait != nil {
			opts.afterWait()
		}
		// Classify cancellation from whether cmd.Cancel successfully signaled
		// the process while Wait was active, not from ctx directly: a Ctrl+C
		// landing after Wait has returned must not relabel a finished run as
		// canceled. The phase is terminal now, so the pre-reap answer is kept.
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
			res.Canceled = canceledExit(exitErr, cancelRequested)
		default:
			res.ExitCode = -1
			res.Err = waitErr
		}
		if opts.DebugLog != nil {
			fmt.Fprintf(opts.DebugLog, "exit: code=%d canceled=%v err=%v\n", res.ExitCode, res.Canceled, res.Err)
		}
		result <- res
	}()

	return run, nil
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
