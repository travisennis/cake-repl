package cake

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"time"

	"github.com/travisennis/cake-repl/internal/childproc"
)

const (
	// trustOutputLimit bounds `cake trust --status --json` stdout. The output is
	// a diagnostic document of a few fields per authority-bearing file, so this
	// only guards a pathological report. The whole document is kept because it
	// is parsed from its first byte.
	trustOutputLimit = 1 << 20 // 1 MiB

	// trustWaitDelay bounds how long ReadTrustStatus waits for the command's
	// stdout/stderr pipes to close after it exits or the context is canceled.
	// The probe is read-only, so cancellation kills the helper outright; the
	// delay only bounds a helper that ignores termination or a descendant that
	// inherited the pipe. It matches the other read-only helper bounds.
	trustWaitDelay = 3 * time.Second
)

// TrustState is cake's project trust decision for one project snapshot. The
// values are cake's stable diagnostic labels; an unrecognized value is kept so
// the caller can surface it instead of silently treating it as trusted.
type TrustState string

const (
	TrustStateNotRequired TrustState = "not_required"
	TrustStateTrusted     TrustState = "trusted"
	TrustStateUntrusted   TrustState = "untrusted"
	TrustStateChanged     TrustState = "changed"
	TrustStateInvalid     TrustState = "invalid"
)

// TrustAuthority is one authority-bearing project file from trust status.
type TrustAuthority struct {
	Path string
	Keys []string
}

// TrustChange is one file that differs from the trusted snapshot.
type TrustChange struct {
	Path   string
	Change string
}

// TrustStatus is the project configuration trust decision returned by
// `cake trust --status --json`.
type TrustStatus struct {
	State         TrustState
	ProjectRoot   string
	Authority     []TrustAuthority
	Changes       []TrustChange
	InvalidReason string
}

// NeedsAttention reports whether cake will refuse project authority or the
// state is not one this client recognizes. Empty state is malformed and is
// rejected during decoding, so it never reaches this method.
func (s TrustStatus) NeedsAttention() bool {
	return s.State != TrustStateNotRequired && s.State != TrustStateTrusted
}

// TrustOptions configures one project trust-status invocation.
type TrustOptions struct {
	Bin      string
	Cwd      string
	DebugLog io.Writer

	// waitDelay overrides trustWaitDelay so a test can shorten the bound; zero
	// keeps the production default (see ReadTrustStatus).
	waitDelay time.Duration
}

// trustEnvelope is the part of the `cake trust --status --json` diagnostic
// envelope this client reads. Everything else, including a newer
// schema_version, is ignored so a forward-compatible cake keeps working.
type trustEnvelope struct {
	Data struct {
		State         string `json:"state"`
		ProjectRoot   string `json:"project_root"`
		InvalidReason string `json:"invalid_reason"`
		Authority     []struct {
			Path string   `json:"path"`
			Keys []string `json:"keys"`
		} `json:"authority"`
		Changes []struct {
			Path   string `json:"path"`
			Change string `json:"change"`
		} `json:"changes"`
	} `json:"data"`
}

// ReadTrustStatus runs the read-only `cake trust --status --json` command for
// opts.Cwd and returns cake's project trust decision. It never writes a trust
// record or otherwise mutates cake state.
//
// Any failure — a non-zero exit, malformed JSON, or an unusable envelope — is
// returned as an error the caller treats as non-fatal: an older cake without
// the command must not break startup.
func ReadTrustStatus(ctx context.Context, opts TrustOptions) (TrustStatus, error) {
	cmd := exec.CommandContext(ctx, opts.Bin, "trust", "--status", "--json") // #nosec G204 -- opts.Bin intentionally comes from -cake-bin/config.
	if opts.Cwd != "" {
		cmd.Dir = opts.Cwd
	}
	childproc.Isolate(cmd)
	// Cancellation kills the read-only helper's whole process group outright;
	// it holds no state to flush. Treat an already-gone helper as a successful
	// cancellation so exec does not replace the natural exit with its default
	// kill error.
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		err := killGroup(cmd.Process.Pid)
		if processGone(err) {
			return nil
		}
		return err
	}
	// Bound completion: WaitDelay stops Wait from blocking on a process that
	// will not exit or on a pipe a descendant still holds.
	cmd.WaitDelay = opts.waitDelay
	if cmd.WaitDelay <= 0 {
		cmd.WaitDelay = trustWaitDelay
	}
	stdout := &capWriter{limit: trustOutputLimit}
	stderr := &tailBuffer{limit: stderrLimit}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		msg := fmt.Sprintf("running %s trust --status: %v", opts.Bin, err)
		if s := stderr.String(); s != "" {
			msg += ": " + s
		}
		return TrustStatus{}, errors.New(msg)
	}
	if stdout.overflow {
		return TrustStatus{}, fmt.Errorf("%s trust status output exceeded %d bytes", opts.Bin, trustOutputLimit)
	}
	raw := stdout.buf.Bytes()
	if opts.DebugLog != nil {
		fmt.Fprintf(opts.DebugLog, "trust status: %s\n", raw)
	}
	return decodeTrustStatus(raw)
}

// decodeTrustStatus parses the diagnostic envelope. Unknown fields and a newer
// schema_version are ignored (ADR 035). A missing state is malformed output:
// guessing a default could turn an unrecognized cake response into a false
// "trusted" or a false warning.
func decodeTrustStatus(raw []byte) (TrustStatus, error) {
	var env trustEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return TrustStatus{}, fmt.Errorf("decoding trust status output: %w", err)
	}
	if env.Data.State == "" {
		return TrustStatus{}, errors.New("decoding trust status output: missing data.state")
	}
	status := TrustStatus{
		State:         TrustState(env.Data.State),
		ProjectRoot:   env.Data.ProjectRoot,
		InvalidReason: env.Data.InvalidReason,
	}
	for _, source := range env.Data.Authority {
		status.Authority = append(status.Authority, TrustAuthority{
			Path: source.Path,
			Keys: source.Keys,
		})
	}
	for _, change := range env.Data.Changes {
		status.Changes = append(status.Changes, TrustChange{
			Path:   change.Path,
			Change: change.Change,
		})
	}
	return status, nil
}
