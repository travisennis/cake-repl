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
	trustOutputLimit = 1 << 20

	trustWaitDelay = 3 * time.Second
)

type TrustState string

const (
	TrustStateNotRequired TrustState = "not_required"
	TrustStateTrusted     TrustState = "trusted"
	TrustStateUntrusted   TrustState = "untrusted"
	TrustStateChanged     TrustState = "changed"
	TrustStateInvalid     TrustState = "invalid"
)

type TrustAuthority struct {
	Path string
	Keys []string
}

type TrustChange struct {
	Path   string
	Change string
}

type TrustStatus struct {
	State         TrustState
	ProjectRoot   string
	Authority     []TrustAuthority
	Changes       []TrustChange
	InvalidReason string
}

func (s TrustStatus) NeedsAttention() bool {
	return s.State != TrustStateNotRequired && s.State != TrustStateTrusted
}

type TrustOptions struct {
	Bin      string
	Cwd      string
	DebugLog io.Writer

	waitDelay time.Duration
}

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

func ReadTrustStatus(ctx context.Context, opts TrustOptions) (TrustStatus, error) {
	cmd := exec.CommandContext(ctx, opts.Bin, "trust", "--status", "--json") // #nosec G204 -- opts.Bin intentionally comes from -cake-bin/config.
	if opts.Cwd != "" {
		cmd.Dir = opts.Cwd
	}
	childproc.Isolate(cmd)
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
	cmd.WaitDelay = opts.waitDelay
	if cmd.WaitDelay <= 0 {
		cmd.WaitDelay = trustWaitDelay
	}
	stdout := childproc.NewCapWriter(trustOutputLimit)
	stderr := childproc.NewTailBuffer(stderrLimit)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		msg := fmt.Sprintf("running %s trust --status: %v", opts.Bin, err)
		if s := stderr.String(); s != "" {
			msg += ": " + s
		}
		return TrustStatus{}, errors.New(msg)
	}
	if stdout.Overflowed() {
		return TrustStatus{}, fmt.Errorf("%s trust status output exceeded %d bytes", opts.Bin, trustOutputLimit)
	}
	raw := stdout.Bytes()
	if opts.DebugLog != nil {
		fmt.Fprintf(opts.DebugLog, "trust status: %s\n", raw)
	}
	return decodeTrustStatus(raw)
}

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
