package compact

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"

	"github.com/travisennis/cake-repl/internal/childproc"
)

const outputLimit = 1 << 20

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
	if opts.DebugLog != nil {
		fmt.Fprintf(opts.DebugLog, "compaction: session=%s exit=%d canceled=%v err=%v\n",
			opts.SessionID, res.ExitCode, res.Canceled, res.Err)
	}
	if res.ExitCode != 0 || res.Err != nil {
		return res
	}
	if execution.Overflowed {
		res.Err = fmt.Errorf("compactor output exceeded %d bytes", outputLimit)
		return res
	}
	var doc Document
	if err := json.Unmarshal(execution.Stdout, &doc); err != nil {
		res.Err = fmt.Errorf("decoding compactor output: %w", err)
		return res
	}
	res.Document = &doc
	return res
}
