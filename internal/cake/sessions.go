package cake

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/travisennis/cake-repl/internal/childproc"
)

const sessionsOutputLimit = 8 << 20

type SessionSummary struct {
	SessionID   string
	FirstPrompt string
	Timestamp   time.Time
}

type SessionsOptions struct {
	Bin      string
	Cwd      string
	DebugLog io.Writer
}

type sessionsEnvelope struct {
	Data struct {
		Sessions []struct {
			SessionID   string `json:"session_id"`
			FirstPrompt string `json:"first_prompt"`
			Timestamp   string `json:"timestamp"`
		} `json:"sessions"`
	} `json:"data"`
}

func ListSessions(ctx context.Context, opts SessionsOptions) ([]SessionSummary, error) {
	res := childproc.Execute(childproc.ExecuteOptions{
		Context:            ctx,
		Bin:                opts.Bin,
		Args:               []string{"sessions", "list", "--json"},
		Cwd:                opts.Cwd,
		ImmediateGroupKill: true,
		OutputLimit:        sessionsOutputLimit,
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
		msg := fmt.Sprintf("running %s sessions list: %v", opts.Bin, cause)
		if res.Stderr != "" {
			msg += ": " + res.Stderr
		}
		return nil, errors.New(msg)
	}
	if res.Overflowed {
		return nil, fmt.Errorf("%s sessions list output exceeded %d bytes", opts.Bin, sessionsOutputLimit)
	}
	if opts.DebugLog != nil {
		fmt.Fprintf(opts.DebugLog, "sessions list: %s\n", res.Stdout)
	}
	return decodeSessions(res.Stdout)
}

func decodeSessions(raw []byte) ([]SessionSummary, error) {
	var env sessionsEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, fmt.Errorf("decoding sessions list output: %w", err)
	}
	rows := env.Data.Sessions
	if len(rows) == 0 {
		return nil, nil
	}
	out := make([]SessionSummary, 0, len(rows))
	for _, r := range rows {
		if r.SessionID == "" {
			continue
		}
		s := SessionSummary{SessionID: r.SessionID, FirstPrompt: r.FirstPrompt}
		if ts, err := time.Parse(time.RFC3339, r.Timestamp); err == nil {
			s.Timestamp = ts
		}
		out = append(out, s)
	}
	return out, nil
}
