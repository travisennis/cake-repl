package compact

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"runtime"
	"strconv"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/travisennis/cake-repl/internal/childproc"
)

const (
	outputLimit = 1 << 20
	stderrLimit = 8 * 1024
	waitDelay   = 3 * time.Second
)

type Options struct {
	Bin                       string
	Cwd                       string
	SessionID                 string
	CakeBin                   string
	ThresholdTokens           int
	ThresholdPercent          int
	KeepTokens                int
	SummarizerModel           string
	SummarizerProfile         string
	SummarizerReasoningEffort string
	DebugLog                  io.Writer

	waitDelay time.Duration
}

func (o Options) Args() []string {
	args := []string{"compact", "-json", "-threshold"}
	switch {
	case o.ThresholdTokens > 0:
		args = append(args, "-threshold-tokens", strconv.Itoa(o.ThresholdTokens))
	case o.ThresholdPercent > 0:
		args = append(args, "-threshold-percent", strconv.Itoa(o.ThresholdPercent))
	}
	if o.KeepTokens > 0 {
		args = append(args, "-keep-tokens", strconv.Itoa(o.KeepTokens))
	}
	if o.CakeBin != "" {
		args = append(args, "-cake-bin", o.CakeBin)
	}
	if o.SummarizerModel != "" {
		args = append(args, "-summarizer-model", o.SummarizerModel)
	}
	if o.SummarizerProfile != "" {
		args = append(args, "-summarizer-profile", o.SummarizerProfile)
	}
	if o.SummarizerReasoningEffort != "" {
		args = append(args, "-summarizer-reasoning-effort", o.SummarizerReasoningEffort)
	}
	return append(args, "--", o.SessionID)
}

type Document struct {
	Compacted       bool   `json:"compacted"`
	Reason          string `json:"reason"`
	SourceSessionID string `json:"source_session_id"`
	SessionID       string `json:"session_id"`
	SessionFile     string `json:"session_file"`
	InPlace         bool   `json:"in_place"`
	CheckpointID    string `json:"checkpoint_id"`
	Boundary        int    `json:"boundary"`
	BeforeTokens    int    `json:"before_tokens"`
	AfterTokens     int    `json:"after_tokens"`
	Window          int    `json:"window"`
	RetainedCount   int    `json:"retained_count"`
}

type Result struct {
	Document *Document
	Stderr   string
	ExitCode int
	Canceled bool
	Err      error
}

func Compact(ctx context.Context, opts Options) Result {
	if opts.Bin == "" {
		return Result{ExitCode: -1, Err: errors.New("no compactor executable configured")}
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
	if opts.DebugLog != nil {
		fmt.Fprintf(opts.DebugLog, "compaction: session=%s exit=%d canceled=%v err=%v\n",
			opts.SessionID, res.ExitCode, res.Canceled, res.Err)
	}
	if res.ExitCode != 0 || res.Err != nil {
		return res
	}
	if stdout.Overflowed() {
		res.Err = fmt.Errorf("compactor output exceeded %d bytes", outputLimit)
		return res
	}
	var doc Document
	if err := json.Unmarshal(stdout.Bytes(), &doc); err != nil {
		res.Err = fmt.Errorf("decoding compactor output: %w", err)
		return res
	}
	res.Document = &doc
	return res
}
