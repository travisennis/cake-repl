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

const sessionsOutputLimit = 8 << 20

const sessionsWaitDelay = 3 * time.Second

type SessionSummary struct {
	SessionID   string
	FirstPrompt string
	Timestamp   time.Time
}

type SessionsOptions struct {
	Bin      string
	Cwd      string
	DebugLog io.Writer

	waitDelay time.Duration
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
	cmd := exec.CommandContext(ctx, opts.Bin, "sessions", "list", "--json") // #nosec G204 -- opts.Bin intentionally comes from -cake-bin/config.
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
		cmd.WaitDelay = sessionsWaitDelay
	}
	stdout := childproc.NewCapWriter(sessionsOutputLimit)
	stderr := childproc.NewTailBuffer(stderrLimit)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		msg := fmt.Sprintf("running %s sessions list: %v", opts.Bin, err)
		if s := stderr.String(); s != "" {
			msg += ": " + s
		}
		return nil, errors.New(msg)
	}
	if stdout.Overflowed() {
		return nil, fmt.Errorf("%s sessions list output exceeded %d bytes", opts.Bin, sessionsOutputLimit)
	}
	raw := stdout.Bytes()
	if opts.DebugLog != nil {
		fmt.Fprintf(opts.DebugLog, "sessions list: %s\n", raw)
	}
	return decodeSessions(raw)
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
