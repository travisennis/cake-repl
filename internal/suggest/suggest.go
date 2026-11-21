package suggest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"runtime"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/travisennis/cake-repl/internal/childproc"
)

const (
	outputLimit = 64 * 1024
	stderrLimit = 8 * 1024
	waitDelay   = 3 * time.Second
)

type Options struct {
	Bin             string
	Cwd             string
	SessionID       string
	CakeBin         string
	Model           string
	Profile         string
	ReasoningEffort string
	DebugLog        io.Writer

	waitDelay time.Duration
}

func (o Options) Args() []string {
	args := []string{"suggest", "-json"}
	if o.CakeBin != "" {
		args = append(args, "-cake-bin", o.CakeBin)
	}
	if o.Model != "" {
		args = append(args, "-model", o.Model)
	}
	if o.Profile != "" {
		args = append(args, "-profile", o.Profile)
	}
	if o.ReasoningEffort != "" {
		args = append(args, "-reasoning-effort", o.ReasoningEffort)
	}
	return append(args, "--", o.SessionID)
}

type Document struct {
	Suggested  *bool  `json:"suggested"`
	Suggestion string `json:"suggestion"`
	SessionID  string `json:"session_id"`
	Reason     string `json:"reason"`
}

type Result struct {
	Document *Document
	Stderr   string
	ExitCode int
	Canceled bool
	Err      error
}

func Suggest(ctx context.Context, opts Options) Result {
	if opts.Bin == "" {
		return Result{ExitCode: -1, Err: errors.New("no suggester executable configured")}
	}
	cmd := exec.CommandContext(ctx, opts.Bin, opts.Args()...) // #nosec G204 -- the executable intentionally comes from config.
	if opts.Cwd != "" {
		cmd.Dir = opts.Cwd
	}
	childproc.Isolate(cmd)
	var cancelRequested atomic.Bool
	cmd.Cancel = func() error {
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
	cmd.WaitDelay = opts.waitDelay
	if cmd.WaitDelay <= 0 {
		cmd.WaitDelay = waitDelay
	}

	stdout := childproc.NewCapWriter(outputLimit)
	stderr := childproc.NewTailBuffer(stderrLimit)
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	err := cmd.Run()
	outcome := childproc.Classify(cmd, err, ctx.Err(), cancelRequested.Load(), stderr)
	res := Result{
		ExitCode: outcome.ExitCode,
		Stderr:   outcome.Stderr,
		Canceled: outcome.Canceled,
		Err:      outcome.Err,
	}
	if outcome.TimedOut {
		cause := ctx.Err()
		if cause == nil {
			cause = err
		}
		res.Canceled = false
		res.Err = fmt.Errorf("suggester timed out: %w", cause)
	}
	if opts.DebugLog != nil {
		fmt.Fprintf(opts.DebugLog, "suggestion: session=%s exit=%d canceled=%v err=%v\n",
			opts.SessionID, res.ExitCode, res.Canceled, res.Err)
	}
	if res.ExitCode != 0 || res.Err != nil {
		return res
	}
	if stdout.Overflowed() {
		res.Err = fmt.Errorf("suggester output exceeded %d bytes", outputLimit)
		return res
	}
	var doc Document
	if err := json.Unmarshal(stdout.Bytes(), &doc); err != nil {
		res.Err = fmt.Errorf("decoding suggester output: %w", err)
		return res
	}
	res.Document = &doc
	return res
}
