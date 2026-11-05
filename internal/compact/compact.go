// Package compact runs the external cake-compact binary and decodes its
// one-document result. It is the only place cake-repl invokes the compactor,
// mirroring the engine boundary in internal/cake: the compactor is an external
// process with a documented JSON contract, and the REPL never reads or writes
// cake session files itself. See ADR 023.
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
	// outputLimit bounds the result document. It is metadata only (ids, counts,
	// estimates), so this only guards against a pathological or wrong binary.
	outputLimit = 1 << 20 // 1 MiB
	// stderrLimit bounds the retained error tail, matching the cake runner.
	stderrLimit = 8 * 1024
	// waitDelay bounds teardown after cancellation or exit: a compactor that
	// ignores SIGTERM, or a summarizer descendant that inherited stdout, cannot
	// hang the REPL. It matches the live-run grace window.
	waitDelay = 3 * time.Second
)

// Options configures one `cake-compact compact` invocation.
type Options struct {
	// Bin is the compactor executable.
	Bin string
	// Cwd is the working directory the compactor runs in, so the compactor
	// resolves its own relative -cake-bin and cake data root against it.
	Cwd string
	// SessionID is the session to compact in place.
	SessionID string
	// CakeBin is passed through as -cake-bin; empty lets the compactor resolve
	// its own default the same way cake-repl did.
	CakeBin string
	// ThresholdTokens is an absolute trigger for the compactor. When positive it
	// is passed as -threshold-tokens and ThresholdPercent is omitted, so exactly
	// one threshold mode reaches the compactor. Zero falls back to
	// ThresholdPercent.
	ThresholdTokens int
	// ThresholdPercent and KeepTokens are passed through. Zero omits the flag
	// and lets the compactor apply its own default.
	ThresholdPercent int
	KeepTokens       int
	// SummarizerModel, SummarizerProfile, and SummarizerReasoningEffort are what
	// cake-repl was configured with for the summary. Empty values leave the
	// compactor's summarizer default, so the summary runs on the same selection
	// the REPL would use for a turn unless compaction overrides it.
	SummarizerModel           string
	SummarizerProfile         string
	SummarizerReasoningEffort string
	// DebugLog receives a content-free lifecycle line when set.
	DebugLog io.Writer

	// waitDelay overrides the teardown bound; zero keeps the production value.
	waitDelay time.Duration
}

// Args returns the compactor CLI arguments.
func (o Options) Args() []string {
	args := []string{"compact", "-json", "-threshold"}
	// Exactly one threshold mode: the absolute line when configured, otherwise
	// the window percentage. Passing both would make the compactor silently
	// prefer the absolute value, obscuring which policy the REPL used.
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
	// The session argument may be a path, so end flag parsing before it.
	return append(args, "--", o.SessionID)
}

// Document is the documented `cake-compact compact -json` result the REPL
// consumes. Unknown fields are ignored so a newer compactor keeps working. The
// compactor reports in_place explicitly; the REPL never assumes the default.
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

// Result is the terminal state of one compaction attempt. Document is non-nil
// only when the compactor exited 0 and emitted a decodable document.
type Result struct {
	Document *Document
	Stderr   string
	ExitCode int
	Canceled bool
	Err      error
}

// Compact runs the compactor until it exits or ctx is canceled. It never
// panics and always returns a Result; a missing binary is reported as an Err.
func Compact(ctx context.Context, opts Options) Result {
	if opts.Bin == "" {
		return Result{ExitCode: -1, Err: errors.New("no compactor executable configured")}
	}
	cmd := exec.CommandContext(ctx, opts.Bin, opts.Args()...) // #nosec G204 -- the executable intentionally comes from config.
	if opts.Cwd != "" {
		cmd.Dir = opts.Cwd
	}
	// The compactor is another child that must not reach the REPL's terminal:
	// it runs on the same session boundary as a cake run. Its SIGTERM-to-leader
	// cancellation and WaitDelay teardown are unaffected by the detach.
	childproc.Isolate(cmd)
	// Prefer a graceful stop so the compactor can abandon an incomplete append
	// and cancel its summarizer; WaitDelay then forces the issue. Windows
	// cannot deliver SIGTERM, so kill outright there.
	//
	// cancelRequested records whether the watcher actually signaled the child.
	// Cancellation is classified from this flag plus the final process status,
	// not from ctx.Err(), so a Ctrl+C that lands after a natural exit cannot
	// relabel a real failure as a cancellation (the same ordering as runner.go).
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
	// A process that exited cleanly is not a cancellation, even if a late
	// Ctrl+C also flipped the context; a completed document wins.
	outcome := childproc.Classify(cmd, err, ctx.Err(), cancelRequested.Load(), stderr)
	res := Result{
		ExitCode: outcome.ExitCode,
		Stderr:   outcome.Stderr,
		Canceled: outcome.Canceled,
		Err:      outcome.Err,
	}
	if opts.DebugLog != nil {
		// Metadata only: the compactor's stderr may contain session content and
		// is already bounded for the failure warning. The owner-only debug log
		// records the lifecycle, not a second copy of the diagnostic.
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
