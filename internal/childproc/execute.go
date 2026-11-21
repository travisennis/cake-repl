package childproc

import (
	"context"
	"io"
	"sync/atomic"
	"time"
)

type ExecuteOptions struct {
	Context            context.Context
	Bin                string
	Args               []string
	Cwd                string
	WaitDelay          time.Duration
	ImmediateGroupKill bool
	OutputLimit        int
	StderrLimit        int
	DebugLog           io.Writer
}

type Execution struct {
	Stdout     []byte
	Overflowed bool
	Outcome
}

func Execute(opts ExecuteOptions) Execution {
	ctx := opts.Context
	if ctx == nil {
		ctx = context.Background()
	}
	var canceled atomic.Bool
	cmd := NewCommand(ctx, opts.Bin, opts.Args, opts.Cwd, CommandOptions{
		WaitDelay:          opts.WaitDelay,
		ImmediateGroupKill: opts.ImmediateGroupKill,
		OnCancel: func() bool {
			canceled.Store(true)
			return true
		},
		DebugLog: opts.DebugLog,
	})

	outputLimit := opts.OutputLimit
	if outputLimit <= 0 {
		outputLimit = 1 << 20
	}
	stderrLimit := opts.StderrLimit
	if stderrLimit <= 0 {
		stderrLimit = DefaultStderr
	}
	stdout := NewCapWriter(outputLimit)
	stderr := NewTailBuffer(stderrLimit)
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	err := cmd.Run()
	cmd.Reaped()
	if ctx.Err() != nil {
		canceled.Store(true)
	}
	outcome := Classify(cmd.Cmd, err, ctx.Err(), canceled.Load(), stderr)
	return Execution{
		Stdout:     stdout.Bytes(),
		Overflowed: stdout.Overflowed(),
		Outcome:    outcome,
	}
}
