package suggest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/travisennis/cake-repl/internal/childproc"
)

const outputLimit = 64 * 1024

type Options struct {
	Bin             string
	Cwd             string
	SessionID       string
	CakeBin         string
	Model           string
	Profile         string
	ReasoningEffort string
	DebugLog        io.Writer
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
	execution := childproc.Execute(childproc.ExecuteOptions{
		Context:     ctx,
		Bin:         opts.Bin,
		Args:        opts.Args(),
		Cwd:         opts.Cwd,
		OutputLimit: outputLimit,
		DebugLog:    opts.DebugLog,
	})
	res := Result{
		ExitCode: execution.ExitCode,
		Stderr:   execution.Stderr,
		Canceled: execution.Canceled,
		Err:      execution.Err,
	}
	if execution.TimedOut {
		cause := ctx.Err()
		if cause == nil {
			cause = execution.Err
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
	if execution.Overflowed {
		res.Err = fmt.Errorf("suggester output exceeded %d bytes", outputLimit)
		return res
	}
	var doc Document
	if err := json.Unmarshal(execution.Stdout, &doc); err != nil {
		res.Err = fmt.Errorf("decoding suggester output: %w", err)
		return res
	}
	res.Document = &doc
	return res
}
