package cake

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func listSessions(t *testing.T, opts SessionsOptions) ([]SessionSummary, error) {
	t.Helper()
	return ListSessions(context.Background(), opts)
}

func TestListSessionsValidEnvelope(t *testing.T) {
	bin := writeFakeCake(t, `
cat <<'EOF'
{
  "schema_version": 1,
  "command": "sessions list",
  "status": "ok",
  "summary": { "count": 2 },
  "checks": [],
  "data": {
    "sessions": [
      {"session_id":"11111111-2222-3333-4444-555555555555","first_prompt":"first","timestamp":"2026-10-07T11:20:44.141622Z"},
      {"session_id":"aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee","first_prompt":"second","timestamp":"2026-10-06T09:00:00Z"}
    ]
  }
}
EOF
exit 0
`)
	sessions, err := listSessions(t, SessionsOptions{Bin: bin})
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(sessions) != 2 {
		t.Fatalf("got %d sessions, want 2: %#v", len(sessions), sessions)
	}
	first := sessions[0]
	if first.SessionID != "11111111-2222-3333-4444-555555555555" {
		t.Errorf("session id = %q", first.SessionID)
	}
	if first.FirstPrompt != "first" {
		t.Errorf("first prompt = %q", first.FirstPrompt)
	}
	want := time.Date(2026, 10, 7, 11, 20, 44, 141622000, time.UTC)
	if !first.Timestamp.Equal(want) {
		t.Errorf("timestamp = %v, want %v", first.Timestamp, want)
	}
}

func TestListSessionsEmpty(t *testing.T) {
	bin := writeFakeCake(t, `
echo '{"schema_version":1,"command":"sessions list","status":"ok","summary":{"count":0},"data":{"sessions":[]}}'
exit 0
`)
	sessions, err := listSessions(t, SessionsOptions{Bin: bin})
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(sessions) != 0 {
		t.Fatalf("got %d sessions, want 0", len(sessions))
	}
}

func TestListSessionsMalformedJSON(t *testing.T) {
	bin := writeFakeCake(t, `
echo 'this is not json'
exit 0
`)
	if _, err := listSessions(t, SessionsOptions{Bin: bin}); err == nil {
		t.Fatal("malformed JSON should return an error")
	}
}

func TestListSessionsNonZeroExit(t *testing.T) {
	bin := writeFakeCake(t, `
echo "sessions list broke" >&2
exit 2
`)
	_, err := listSessions(t, SessionsOptions{Bin: bin})
	if err == nil {
		t.Fatal("non-zero exit should return an error")
	}
	if !strings.Contains(err.Error(), "sessions list broke") {
		t.Errorf("error should carry the stderr tail, got %q", err)
	}
}

// TestListSessionsUnknownSchemaVersionIgnored pins the forward-compatibility
// rule from ADR 016: a newer schema_version is ignored, not treated as a hard
// error, so a compatible future envelope still lists.
func TestListSessionsUnknownSchemaVersionIgnored(t *testing.T) {
	bin := writeFakeCake(t, `
echo '{"schema_version":2,"command":"sessions list","status":"ok","data":{"sessions":[{"session_id":"11111111-2222-3333-4444-555555555555","first_prompt":"hi","timestamp":"2026-10-07T11:20:44Z"}]}}'
exit 0
`)
	sessions, err := listSessions(t, SessionsOptions{Bin: bin})
	if err != nil {
		t.Fatalf("a newer schema_version should be ignored, got %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("got %d sessions, want 1", len(sessions))
	}
}

// TestListSessionsSkipAndTolerantFields covers the row-level tolerances: an
// entry with no session id is dropped (it could never be resumed) and an
// unparseable timestamp leaves the zero time rather than failing the listing.
func TestListSessionsSkipAndTolerantFields(t *testing.T) {
	bin := writeFakeCake(t, `
echo '{"schema_version":1,"data":{"sessions":[{"first_prompt":"no id","timestamp":"2026-10-07T11:20:44Z"},{"session_id":"11111111-2222-3333-4444-555555555555","first_prompt":"kept","timestamp":"not-a-time"}]}}'
exit 0
`)
	sessions, err := listSessions(t, SessionsOptions{Bin: bin})
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("got %d sessions, want 1: %#v", len(sessions), sessions)
	}
	if !sessions[0].Timestamp.IsZero() {
		t.Errorf("unparseable timestamp should stay zero, got %v", sessions[0].Timestamp)
	}
}

// TestListSessionsInvokesReadOnlyCommand checks the argument contract: exactly
// `sessions list --json`, with no prompt and no --output-format.
func TestListSessionsInvokesReadOnlyCommand(t *testing.T) {
	argsPath := filepath.Join(t.TempDir(), "args")
	t.Setenv("ARGS_FILE", argsPath)
	bin := writeFakeCake(t, `
printf '%s\n' "$@" >> "$ARGS_FILE"
echo '{"schema_version":1,"data":{"sessions":[]}}'
exit 0
`)
	if _, err := listSessions(t, SessionsOptions{Bin: bin}); err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	args, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("reading captured args: %v", err)
	}
	if got := string(args); got != "sessions\nlist\n--json\n" {
		t.Errorf("args = %q, want %q", got, "sessions\nlist\n--json\n")
	}
}

// TestListSessionsUsesCwd checks that the listing runs in the requested
// directory, so the sessions it returns match the directory of live prompts.
func TestListSessionsUsesCwd(t *testing.T) {
	dir := t.TempDir()
	pwdPath := filepath.Join(t.TempDir(), "pwd")
	t.Setenv("PWD_FILE", pwdPath)
	bin := writeFakeCake(t, `
printf '%s' "$PWD" > "$PWD_FILE"
echo '{"schema_version":1,"data":{"sessions":[]}}'
exit 0
`)
	if _, err := listSessions(t, SessionsOptions{Bin: bin, Cwd: dir}); err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	got, err := os.ReadFile(pwdPath)
	if err != nil {
		t.Fatalf("reading recorded pwd: %v", err)
	}
	// macOS temp dirs are symlinked (/var -> /private/var); compare the
	// resolved paths.
	wantResolved, _ := filepath.EvalSymlinks(dir)
	gotResolved, _ := filepath.EvalSymlinks(string(got))
	if gotResolved != wantResolved {
		t.Errorf("cwd = %q, want %q", gotResolved, wantResolved)
	}
}

func TestListSessionsDebugLogWritesRawOutput(t *testing.T) {
	bin := writeFakeCake(t, `
echo '{"schema_version":1,"data":{"sessions":[]}}'
exit 0
`)
	var log bytes.Buffer
	if _, err := listSessions(t, SessionsOptions{Bin: bin, DebugLog: &log}); err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if !strings.Contains(log.String(), `"schema_version"`) {
		t.Errorf("raw envelope not written to the debug log: %q", log.String())
	}
}

// waitForFile blocks until path exists, so a test acts only after the fake cake
// has actually started rather than racing its startup.
func waitForFile(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(path); err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("fake cake never started: %s", path)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// killPIDFile kills the process id recorded in path, so a fake helper that holds
// a pipe open cannot outlive the test that spawned it.
func killPIDFile(t *testing.T, path string) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		return
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil || pid <= 0 {
		return
	}
	if p, err := os.FindProcess(pid); err == nil {
		_ = p.Kill()
	}
}

// TestListSessionsCancellationStopsHelper: canceling the listing context stops a
// helper that would otherwise run for a long time.
func TestListSessionsCancellationStopsHelper(t *testing.T) {
	started := filepath.Join(t.TempDir(), "started")
	t.Setenv("STARTED_FILE", started)
	bin := writeFakeCake(t, `
touch "$STARTED_FILE"
exec sleep 30
`)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		_, err := ListSessions(ctx, SessionsOptions{Bin: bin})
		done <- err
	}()
	waitForFile(t, started)

	start := time.Now()
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Error("a canceled listing should return an error")
		}
		if elapsed := time.Since(start); elapsed > 10*time.Second {
			t.Errorf("canceled listing took %s, want it to stop promptly", elapsed)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("canceled listing never returned")
	}
}

// TestListSessionsBoundsDescendantHoldingStdout: a helper that exits but leaves a
// descendant holding the inherited stdout pipe must not hang the listing. The
// short WaitDelay closes the pipe and surfaces the bounded teardown as a
// non-fatal error. The fake uses finite helpers and the test kills the sleeper.
func TestListSessionsBoundsDescendantHoldingStdout(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "pid")
	t.Setenv("PID_FILE", pidFile)
	bin := writeFakeCake(t, `
sleep 30 &
echo $! > "$PID_FILE"
echo '{"schema_version":1,"data":{"sessions":[]}}'
exit 0
`)
	t.Cleanup(func() { killPIDFile(t, pidFile) })

	start := time.Now()
	_, err := ListSessions(context.Background(), SessionsOptions{
		Bin:       bin,
		waitDelay: 200 * time.Millisecond,
	})
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("listing waited %s for a descendant-held pipe, want a bounded return", elapsed)
	}
	if err == nil {
		t.Error("a descendant-held pipe should surface as a non-fatal listing error")
	}
}

// TestListSessionsEnforcesStdoutCap: the 8 MiB stdout cap survives the lifecycle
// change, so a pathological helper fails the listing instead of buffering without
// bound.
func TestListSessionsEnforcesStdoutCap(t *testing.T) {
	bin := writeFakeCake(t, `
dd if=/dev/zero bs=1048576 count=9 2>/dev/null
exit 0
`)
	_, err := ListSessions(context.Background(), SessionsOptions{Bin: bin})
	if err == nil {
		t.Fatal("output over the cap should be an error")
	}
	if !strings.Contains(err.Error(), "exceeded") {
		t.Errorf("error = %q, want it to name the output cap", err)
	}
}
