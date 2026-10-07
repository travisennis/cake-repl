package cake

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"time"
)

// sessionsOutputLimit bounds how many bytes of `cake sessions list --json`
// stdout are buffered. A listing is a few fields per session, so this only
// guards against a pathological directory: once the limit is passed the
// remaining output is discarded and the call fails, and the caller degrades to
// a non-fatal warning.
const sessionsOutputLimit = 8 << 20 // 8 MiB

// SessionSummary is one session returned by `cake sessions list --json`.
type SessionSummary struct {
	SessionID   string
	FirstPrompt string
	Timestamp   time.Time
}

// SessionsOptions configures one session-listing invocation.
type SessionsOptions struct {
	Bin      string
	Cwd      string
	DebugLog io.Writer
}

// sessionsEnvelope is the part of the `cake sessions list --json` envelope this
// client reads. Only data.sessions is used: everything else, including a newer
// schema_version, is ignored so a forward-compatible cake keeps working
// (ADR 016).
type sessionsEnvelope struct {
	Data struct {
		Sessions []struct {
			SessionID   string `json:"session_id"`
			FirstPrompt string `json:"first_prompt"`
			Timestamp   string `json:"timestamp"`
		} `json:"sessions"`
	} `json:"data"`
}

// ListSessions runs the read-only `cake sessions list --json` command and
// returns the sessions belonging to opts.Cwd. It never mutates cake state.
//
// Any failure — a non-zero exit, malformed JSON, or an unreadable envelope — is
// returned as an error the caller treats as non-fatal: the REPL shows a warning
// and keeps accepting input.
func ListSessions(ctx context.Context, opts SessionsOptions) ([]SessionSummary, error) {
	cmd := exec.CommandContext(ctx, opts.Bin, "sessions", "list", "--json") // #nosec G204 -- opts.Bin intentionally comes from -cake-bin/config.
	if opts.Cwd != "" {
		cmd.Dir = opts.Cwd
	}
	stdout := &capWriter{limit: sessionsOutputLimit}
	stderr := &tailBuffer{limit: stderrLimit}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		msg := fmt.Sprintf("running %s sessions list: %v", opts.Bin, err)
		if s := stderr.String(); s != "" {
			msg += ": " + s
		}
		return nil, errors.New(msg)
	}
	if stdout.overflow {
		return nil, fmt.Errorf("%s sessions list output exceeded %d bytes", opts.Bin, sessionsOutputLimit)
	}
	raw := stdout.buf.Bytes()
	if opts.DebugLog != nil {
		fmt.Fprintf(opts.DebugLog, "sessions list: %s\n", raw)
	}
	return decodeSessions(raw)
}

// decodeSessions parses the listing envelope. A malformed envelope is an error;
// within a well-formed one, an entry is skipped only when it carries no
// session id (it could never be resumed), and an unparseable timestamp leaves
// the zero time so one odd row cannot hide the rest of the listing.
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

// capWriter is an io.Writer that keeps at most limit bytes and records whether
// more arrived. Unlike tailBuffer it keeps the head, because the listing is a
// single JSON object that must be parsed from its first byte.
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
