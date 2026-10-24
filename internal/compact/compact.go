// Package compact runs the external cake-compact binary and decodes its
// one-document result. It is the only place cake-repl invokes the compactor,
// mirroring the engine boundary in internal/cake: the compactor is an external
// process with a documented JSON contract, and the REPL never reads or writes
// cake session files itself. See ADR 023.
package compact

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"
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
	// Cwd is the working directory the compactor runs in, so a relative
	// cake-bin resolves the same way it does for a live cake run.
	Cwd string
	// SessionID is the session to compact in place.
	SessionID string
	// CakeBin is passed through as -cake-bin; empty lets the compactor resolve
	// its own default the same way cake-repl did.
	CakeBin string
	// ThresholdPercent and KeepTokens are passed through. Zero omits the flag
	// and lets the compactor apply its own default.
	ThresholdPercent int
	KeepTokens       int
	// SummarizerModel is the model cake-repl was configured with. Empty leaves
	// the compactor's summarizer default.
	SummarizerModel string
	// DebugLog receives a content-free lifecycle line when set.
	DebugLog io.Writer

	// waitDelay overrides the teardown bound; zero keeps the production value.
	waitDelay time.Duration
}

// Args returns the compactor CLI arguments.
func (o Options) Args() []string {
	args := []string{"compact", "-json", "-threshold"}
	if o.ThresholdPercent > 0 {
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
	// The session argument may be a path, so end flag parsing before it.
	return append(args, "--", o.SessionID)
}

// Document is the subset of `cake-compact compact -json` output the REPL reads.
// Unknown fields are ignored so a newer compactor keeps working. The compactor
// reports in_place explicitly; the REPL never assumes the default.
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
	// Prefer a graceful stop so the compactor can abandon an incomplete append
	// and cancel its summarizer; WaitDelay then forces the issue. Windows
	// cannot deliver SIGTERM, so kill outright there.
	cmd.Cancel = func() error {
		if runtime.GOOS == "windows" {
			return cmd.Process.Kill()
		}
		return cmd.Process.Signal(syscall.SIGTERM)
	}
	cmd.WaitDelay = opts.waitDelay
	if cmd.WaitDelay <= 0 {
		cmd.WaitDelay = waitDelay
	}

	stdout := &capWriter{limit: outputLimit}
	stderr := &tailBuffer{limit: stderrLimit}
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	res := Result{ExitCode: -1}
	err := cmd.Run()
	res.Stderr = stderr.String()
	res.Canceled = ctx.Err() != nil
	var exitErr *exec.ExitError
	switch {
	case err == nil:
		res.ExitCode = 0
	case errors.Is(err, context.Canceled):
		res.ExitCode = exitCode(cmd)
	case errors.As(err, &exitErr):
		res.ExitCode = exitCode(cmd)
	default:
		// The process could not be started (a missing or non-executable
		// binary) or teardown failed in another way.
		res.Err = err
	}
	if opts.DebugLog != nil {
		fmt.Fprintf(opts.DebugLog, "compaction: session=%s exit=%d canceled=%v err=%v stderr=%s\n",
			opts.SessionID, res.ExitCode, res.Canceled, res.Err, res.Stderr)
	}
	if res.ExitCode != 0 || res.Err != nil {
		return res
	}
	if stdout.overflow {
		res.Err = fmt.Errorf("compactor output exceeded %d bytes", outputLimit)
		return res
	}
	var doc Document
	if err := json.Unmarshal(stdout.buf.Bytes(), &doc); err != nil {
		res.Err = fmt.Errorf("decoding compactor output: %w", err)
		return res
	}
	res.Document = &doc
	return res
}

func exitCode(cmd *exec.Cmd) int {
	if cmd.ProcessState == nil {
		return -1
	}
	return cmd.ProcessState.ExitCode()
}

// capWriter keeps at most limit bytes and records whether more arrived. It
// keeps the head because the document is one JSON object parsed from byte zero.
type capWriter struct {
	limit    int
	buf      bytes.Buffer
	overflow bool
}

func (w *capWriter) Write(p []byte) (int, error) {
	room := w.limit - w.buf.Len()
	if room <= 0 {
		w.overflow = true
		return len(p), nil
	}
	if len(p) > room {
		w.buf.Write(p[:room])
		w.overflow = true
		return len(p), nil
	}
	w.buf.Write(p)
	return len(p), nil
}

// tailBuffer retains the last limit bytes of stderr, which is enough to show a
// diagnostic without letting a chatty or wrong binary grow memory.
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
// dropped output and a cut kept on a rune boundary.
func (b *tailBuffer) String() string {
	buf := b.buf
	if b.truncated {
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
