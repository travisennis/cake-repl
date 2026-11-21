package cake

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/travisennis/cake-repl/internal/childproc"
)

const trustOutputLimit = 1 << 20

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
	res := childproc.Execute(childproc.ExecuteOptions{
		Context:            ctx,
		Bin:                opts.Bin,
		Args:               []string{"trust", "--status", "--json"},
		Cwd:                opts.Cwd,
		ImmediateGroupKill: true,
		OutputLimit:        trustOutputLimit,
		DebugLog:           opts.DebugLog,
	})
	if res.Err != nil || res.ExitCode != 0 {
		cause := res.Err
		switch {
		case res.Canceled:
			cause = errors.New("canceled")
		case cause == nil:
			cause = fmt.Errorf("exit status %d", res.ExitCode)
		}
		msg := fmt.Sprintf("running %s trust --status: %v", opts.Bin, cause)
		if res.Stderr != "" {
			msg += ": " + res.Stderr
		}
		return TrustStatus{}, errors.New(msg)
	}
	if res.Overflowed {
		return TrustStatus{}, fmt.Errorf("%s trust status output exceeded %d bytes", opts.Bin, trustOutputLimit)
	}
	if opts.DebugLog != nil {
		fmt.Fprintf(opts.DebugLog, "trust status: %s\n", res.Stdout)
	}
	return decodeTrustStatus(res.Stdout)
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
