// Package suggest runs the external cake-suggest binary and decodes its
// one-document result. It is the only place cake-repl invokes the suggester,
// mirroring the engine boundary in internal/cake and the compactor boundary in
// internal/compact: the suggester is an external process with a documented JSON
// contract, and the REPL never reads or writes cake session files itself. See
// ADR 028.
package suggest

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"runtime"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/travisennis/cake-repl/internal/childproc"
)

const (
	// outputLimit bounds the result document. A suggestion is one short prompt,
	// so this only guards against a pathological or wrong binary.
	outputLimit = 64 * 1024
	// stderrLimit bounds the retained error tail, matching the cake runner and
	// the compactor.
	stderrLimit = 8 * 1024
	// waitDelay bounds teardown after cancellation or exit: a suggester that
	// ignores SIGTERM, or a descendant that inherited stdout, cannot hang the
	// REPL. It matches the live-run grace window.
	waitDelay = 3 * time.Second
)

// Options configures one `cake-suggest suggest` invocation.
type Options struct {
	// Bin is the suggester executable.
	Bin string
	// Cwd is the working directory the suggester runs in, so it resolves its
	// own relative -cake-bin and cake data root against it.
	Cwd string
	// SessionID is the session to read for the suggestion.
	SessionID string
	// CakeBin is passed through as -cake-bin; empty lets the suggester resolve
	// its own default the same way cake-repl did.
	CakeBin string
	// Model and Profile are the identity cake-repl was configured with, so the
	// suggestion runs the same model path as the REPL unless the suggester
	// chooses its own defaults. Empty values omit the flag.
	Model   string
	Profile string
	// DebugLog receives a content-free lifecycle line when set.
	DebugLog io.Writer

	// waitDelay overrides the teardown bound; zero keeps the production value.
	waitDelay time.Duration
}

// Args returns the suggester CLI arguments.
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
	// The session argument may be a path, so end flag parsing before it.
	return append(args, "--", o.SessionID)
}

// Document is the documented `cake-suggest suggest -json` result the REPL
// consumes. Unknown fields are ignored so a newer suggester keeps working.
// Suggested is a pointer so a missing or null required field is distinguishable
// from an explicit false; the REPL warns on the malformed document instead of
// treating it as a silent no-op. The REPL never treats an empty suggestion as a
// prompt.
type Document struct {
	Suggested  *bool  `json:"suggested"`
	Suggestion string `json:"suggestion"`
	SessionID  string `json:"session_id"`
	Reason     string `json:"reason"`
}

// Result is the terminal state of one suggestion attempt. Document is non-nil
// only when the suggester exited 0 and emitted a decodable document.
type Result struct {
	Document *Document
	Stderr   string
	ExitCode int
	Canceled bool
	Err      error
}

// Suggest runs the suggester until it exits or ctx is canceled. It never panics
// and always returns a Result; a missing binary is reported as an Err.
func Suggest(ctx context.Context, opts Options) Result {
	if opts.Bin == "" {
		return Result{ExitCode: -1, Err: errors.New("no suggester executable configured")}
	}
	cmd := exec.CommandContext(ctx, opts.Bin, opts.Args()...) // #nosec G204 -- the executable intentionally comes from config.
	if opts.Cwd != "" {
		cmd.Dir = opts.Cwd
	}
	// The suggester is another child that must not reach the REPL's terminal:
	// it runs on the same session boundary as a cake run.
	childproc.Isolate(cmd)
	// Prefer a graceful stop so the suggester can abandon its model call and
	// clean up; WaitDelay then forces the issue. Windows cannot deliver SIGTERM,
	// so kill outright there.
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

	stdout := &capWriter{limit: outputLimit}
	stderr := &tailBuffer{limit: stderrLimit}
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	res := Result{ExitCode: -1}
	err := cmd.Run()
	res.Stderr = stderr.String()
	var exitErr *exec.ExitError
	switch {
	case err == nil:
		res.ExitCode = 0
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		// The fixed timeout fired: a hung suggester is a failure, not a user
		// cancellation, and the caller reports it once.
		res.ExitCode = exitCode(cmd)
		res.Err = fmt.Errorf("suggester timed out: %w", ctx.Err())
	case errors.As(err, &exitErr):
		// Classify a non-zero exit from the cancel flag and signal status, not
		// from ctx.Err(), so a late cancel cannot relabel a natural failure and a
		// canceled child that exits non-zero is still silent.
		res.ExitCode = exitCode(cmd)
		res.Canceled = canceledExit(exitErr, cancelRequested.Load())
	case errors.Is(err, context.Canceled):
		// The context canceled before the child produced an exit status. This is
		// an expected dismissal, never a user-visible failure.
		res.ExitCode = exitCode(cmd)
		res.Canceled = true
	default:
		// The process could not be started (a missing or non-executable binary)
		// or teardown failed in another way.
		res.Err = err
	}
	if opts.DebugLog != nil {
		// Metadata only: stderr may contain session content and is already
		// bounded for the one failure warning. The owner-only debug log records
		// the lifecycle, not a second copy of the diagnostic.
		fmt.Fprintf(opts.DebugLog, "suggestion: session=%s exit=%d canceled=%v err=%v\n",
			opts.SessionID, res.ExitCode, res.Canceled, res.Err)
	}
	if res.ExitCode != 0 || res.Err != nil {
		return res
	}
	if stdout.overflow {
		res.Err = fmt.Errorf("suggester output exceeded %d bytes", outputLimit)
		return res
	}
	var doc Document
	if err := json.Unmarshal(stdout.buf.Bytes(), &doc); err != nil {
		res.Err = fmt.Errorf("decoding suggester output: %w", err)
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

// canceledExit reports whether a non-zero exit is a cancellation rather than a
// natural failure. On POSIX, signaling an exited-but-unreaped process can
// succeed, so both the cancel callback and a signal-terminated status are
// required; Windows has no signal status, so a successful cancel is enough.
func canceledExit(exitErr *exec.ExitError, cancelRequested bool) bool {
	if !cancelRequested {
		return false
	}
	return runtime.GOOS == "windows" || !exitErr.Exited()
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
