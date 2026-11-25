package cake

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/travisennis/cake-repl/internal/childproc"
)

type RunMode int

const (
	RunFresh RunMode = iota
	RunResume
)

func (m RunMode) String() string {
	if m == RunResume {
		return "resume"
	}
	return "fresh"
}

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
	Registry     *Registry

	replay bool
}

type ReplayOptions struct {
	Bin       string
	Cwd       string
	SessionID string
	DebugLog  io.Writer
	Registry  *Registry
	Context   context.Context
}

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

func (o Options) Args() []string {
	if o.replay {
		return []string{"--output-format", "stream-json", "replay", o.ResumeID}
	}

	args := []string{"--output-format", "stream-json"}
	switch o.Mode {
	case RunResume:
		args = append(args, "--resume", o.ResumeID)
	case RunFresh:
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
	for _, dir := range o.AddDirs {
		args = append(args, "--add-dir", dir)
	}
	for _, dir := range o.ToolboxDirs {
		args = append(args, "--toolbox", dir)
	}
	if o.Sandbox != "" {
		args = append(args, "--sandbox", o.Sandbox)
	}
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
	return append(args, "--", o.Prompt)
}

type Result struct {
	ExitCode int
	Stderr   string
	Canceled bool
	Err      error
}

const inputErrorExitCode = 3

const contextMismatchStderr = "Session context mismatch: context for session "

func (r Result) ContextMismatch() bool {
	return r.ExitCode == inputErrorExitCode && !r.Canceled && r.Err == nil &&
		strings.Contains(r.Stderr, contextMismatchStderr) &&
		!strings.Contains(r.Stderr, "--strict-context-drift")
}

type ParseError struct {
	Line string
	Err  error
}

func (e ParseError) EventType() string { return "parse_error" }

type processPhase uint32

const (
	processStarting processPhase = iota
	processRunning
	processCancelRequested
	processReaped
)

func (p processPhase) cancelRequested() bool {
	return p == processCancelRequested
}

type Run struct {
	Events <-chan Event
	Result <-chan Result
	Done   <-chan struct{}
	cancel context.CancelFunc
	pid    atomic.Int64
	phase  atomic.Uint32
	cmd    *childproc.Command
}

func (r *Run) currentPhase() processPhase {
	return processPhase(r.phase.Load())
}

func (r *Run) markRunning() {
	if r == nil {
		return
	}
	r.phase.CompareAndSwap(uint32(processStarting), uint32(processRunning))
}

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

func (r *Run) markReaped() {
	if r == nil {
		return
	}
	r.phase.Store(uint32(processReaped))
	if r.cmd != nil {
		r.cmd.Reaped()
	}
}

func (r *Run) Cancel() {
	if r.cancel != nil {
		r.cancel()
	}
}

func Start(opts Options) (*Run, error) {
	return start(context.Background(), opts)
}

func start(parent context.Context, opts Options) (*Run, error) {
	ctx, cancel := context.WithCancel(parent)

	grace := childproc.DefaultGrace
	events := make(chan Event, 64)
	result := make(chan Result, 1)
	done := make(chan struct{})
	run := &Run{Events: events, Result: result, Done: done, cancel: cancel}

	cmd := childproc.NewCommand(ctx, opts.Bin, opts.Args(), opts.Cwd, childproc.CommandOptions{
		WaitDelay: grace + time.Second,
		Grace:     grace,
		OnCancel:  run.beginCancel,
		DebugLog:  opts.DebugLog,
	})
	run.cmd = cmd

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("opening stdout pipe: %w", err)
	}
	stderr := childproc.NewTailBuffer(childproc.DefaultStderr)
	cmd.Stderr = stderr

	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("starting %s: %w", opts.Bin, err)
	}
	run.pid.Store(int64(cmd.Process.Pid))
	run.markRunning()
	if opts.Registry != nil {
		opts.Registry.Add(run)
	}

	go func() {
		defer cancel()
		defer close(events)
		reader := bufio.NewReader(stdout)
		dropping := false
		sendEvent := func(ev Event) {
			if dropping {
				return
			}
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
		outcome := childproc.Classify(cmd.Cmd, waitErr, ctx.Err(), cancelRequested, stderr)
		res := Result{
			ExitCode: outcome.ExitCode,
			Stderr:   outcome.Stderr,
			Canceled: outcome.Canceled,
			Err:      outcome.Err,
		}
		if opts.DebugLog != nil {
			fmt.Fprintf(opts.DebugLog, "exit: code=%d canceled=%v err=%v\n", res.ExitCode, res.Canceled, res.Err)
		}
		result <- res
	}()

	return run, nil
}

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
