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

	afterWait  func()
	afterStart func(*Run)
	replay     bool
	waitDelay  time.Duration
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
	processGraceArmed
	processGroupKilled
	processReaped
)

func (p processPhase) cancelRequested() bool {
	switch p {
	case processCancelRequested, processGraceArmed, processGroupKilled:
		return true
	default:
		return false
	}
}

type Run struct {
	Events <-chan Event
	Result <-chan Result
	Done   <-chan struct{}
	cancel context.CancelFunc
	pid    atomic.Int64
	phase  atomic.Uint32

	mu         sync.Mutex
	graceTimer *time.Timer
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

func (r *Run) armGrace() bool {
	if r == nil {
		return false
	}
	return r.phase.CompareAndSwap(uint32(processCancelRequested), uint32(processGraceArmed))
}

func (r *Run) markGroupKilled() bool {
	if r == nil {
		return false
	}
	return r.phase.CompareAndSwap(uint32(processGraceArmed), uint32(processGroupKilled))
}

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

func (r *Run) Cancel() {
	if r.cancel != nil {
		r.cancel()
	}
}

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

const stderrLimit = 8 * 1024

func Start(opts Options) (*Run, error) {
	return start(context.Background(), opts)
}

func start(parent context.Context, opts Options) (*Run, error) {
	ctx, cancel := context.WithCancel(parent)

	cmd := exec.CommandContext(ctx, opts.Bin, opts.Args()...) // #nosec G204 -- opts.Bin intentionally comes from -cake-bin/config.
	if opts.Cwd != "" {
		cmd.Dir = opts.Cwd
	}
	childproc.Isolate(cmd)

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
	stderr := childproc.NewTailBuffer(stderrLimit)
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
	if run.currentPhase() == processCancelRequested {
		run.armGroupKill(grace, opts.DebugLog)
	}
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
		if opts.afterWait != nil {
			opts.afterWait()
		}
		outcome := childproc.Classify(cmd, waitErr, ctx.Err(), cancelRequested, stderr)
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
