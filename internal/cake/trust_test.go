package cake

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func readTrustStatus(t *testing.T, opts TrustOptions) (TrustStatus, error) {
	t.Helper()
	return ReadTrustStatus(context.Background(), opts)
}

func TestReadTrustStatusUntrustedEnvelope(t *testing.T) {
	bin := writeFakeCake(t, `
cat <<'EOF'
{
  "schema_version": 1,
  "command": "trust status",
  "status": "ok",
  "summary": { "authority": 1, "changes": 0, "state": "untrusted" },
  "checks": [],
  "data": {
    "state": "untrusted",
    "project_root": "/tmp/project",
    "authority": [
      {"path": ".cake/settings.toml", "kind": "settings", "keys": ["system_prompt", "[gates.review]"]}
    ],
    "changes": []
  }
}
EOF
exit 0
`)
	status, err := readTrustStatus(t, TrustOptions{Bin: bin})
	if err != nil {
		t.Fatalf("ReadTrustStatus: %v", err)
	}
	if status.State != TrustStateUntrusted {
		t.Errorf("state = %q, want %q", status.State, TrustStateUntrusted)
	}
	if !status.NeedsAttention() {
		t.Error("untrusted should need attention")
	}
	if status.ProjectRoot != "/tmp/project" {
		t.Errorf("project root = %q", status.ProjectRoot)
	}
	if len(status.Authority) != 1 || status.Authority[0].Path != ".cake/settings.toml" {
		t.Fatalf("authority = %#v", status.Authority)
	}
	if got := strings.Join(status.Authority[0].Keys, ","); got != "system_prompt,[gates.review]" {
		t.Errorf("authority keys = %q", got)
	}
}

func TestReadTrustStatusStates(t *testing.T) {
	tests := []struct {
		name       string
		data       string
		wantState  TrustState
		wantAttn   bool
		wantReason string
		wantChange string
	}{
		{
			name:      "not required",
			data:      `{"state":"not_required","project_root":"/tmp/project","authority":[],"changes":[]}`,
			wantState: TrustStateNotRequired,
		},
		{
			name:      "trusted",
			data:      `{"state":"trusted","project_root":"/tmp/project","authority":[],"changes":[]}`,
			wantState: TrustStateTrusted,
		},
		{
			name:       "changed",
			data:       `{"state":"changed","project_root":"/tmp/project","authority":[],"changes":[{"path":".cake/settings.toml","change":"modified"}]}`,
			wantState:  TrustStateChanged,
			wantAttn:   true,
			wantChange: "modified",
		},
		{
			name:       "invalid",
			data:       `{"state":"invalid","project_root":"/tmp/project","invalid_reason":"the trust record is a symlink","authority":[],"changes":[]}`,
			wantState:  TrustStateInvalid,
			wantAttn:   true,
			wantReason: "the trust record is a symlink",
		},
		{
			name:      "unknown forward-compatible state",
			data:      `{"state":"expired","project_root":"/tmp/project","authority":[],"changes":[]}`,
			wantState: TrustState("expired"),
			wantAttn:  true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw := `{"schema_version":1,"command":"trust status","data":` + tt.data + `}`
			status, err := decodeTrustStatus([]byte(raw))
			if err != nil {
				t.Fatalf("decodeTrustStatus: %v", err)
			}
			if status.State != tt.wantState {
				t.Errorf("state = %q, want %q", status.State, tt.wantState)
			}
			if got := status.NeedsAttention(); got != tt.wantAttn {
				t.Errorf("NeedsAttention = %v, want %v", got, tt.wantAttn)
			}
			if status.InvalidReason != tt.wantReason {
				t.Errorf("invalid reason = %q, want %q", status.InvalidReason, tt.wantReason)
			}
			if tt.wantChange == "" {
				if len(status.Changes) != 0 {
					t.Errorf("changes = %#v, want none", status.Changes)
				}
			} else if len(status.Changes) != 1 || status.Changes[0].Change != tt.wantChange {
				t.Errorf("changes = %#v, want one %q change", status.Changes, tt.wantChange)
			}
		})
	}
}

func TestReadTrustStatusMissingState(t *testing.T) {
	bin := writeFakeCake(t, `
echo '{"schema_version":1,"data":{"project_root":"/tmp/project","authority":[]}}'
exit 0
`)
	if _, err := readTrustStatus(t, TrustOptions{Bin: bin}); err == nil {
		t.Fatal("missing state should return an error")
	}
}

func TestReadTrustStatusMalformedJSON(t *testing.T) {
	bin := writeFakeCake(t, `
echo 'not json'
exit 0
`)
	if _, err := readTrustStatus(t, TrustOptions{Bin: bin}); err == nil {
		t.Fatal("malformed JSON should return an error")
	}
}

func TestReadTrustStatusNonZeroExit(t *testing.T) {
	bin := writeFakeCake(t, `
echo "trust status broke" >&2
exit 3
`)
	_, err := readTrustStatus(t, TrustOptions{Bin: bin})
	if err == nil {
		t.Fatal("non-zero exit should return an error")
	}
	if !strings.Contains(err.Error(), "trust status broke") {
		t.Errorf("error should carry the stderr tail, got %q", err)
	}
}

// TestReadTrustStatusUnknownSchemaVersionIgnored pins the forward-compatibility
// rule from ADR 035: a newer schema_version is ignored, not treated as a hard
// error, so a compatible future envelope still reports the state.
func TestReadTrustStatusUnknownSchemaVersionIgnored(t *testing.T) {
	bin := writeFakeCake(t, `
echo '{"schema_version":2,"command":"trust status","status":"ok","data":{"state":"trusted","project_root":"/tmp/project","future_field":true}}'
exit 0
`)
	status, err := readTrustStatus(t, TrustOptions{Bin: bin})
	if err != nil {
		t.Fatalf("a newer schema_version should be ignored, got %v", err)
	}
	if status.State != TrustStateTrusted {
		t.Errorf("state = %q, want trusted", status.State)
	}
}

// TestReadTrustStatusInvokesReadOnlyCommand checks the argument contract:
// exactly `trust --status --json`, with no prompt and no output-format flag.
func TestReadTrustStatusInvokesReadOnlyCommand(t *testing.T) {
	argsPath := filepath.Join(t.TempDir(), "args")
	t.Setenv("ARGS_FILE", argsPath)
	bin := writeFakeCake(t, `
printf '%s\n' "$@" >> "$ARGS_FILE"
echo '{"schema_version":1,"data":{"state":"trusted"}}'
exit 0
`)
	if _, err := readTrustStatus(t, TrustOptions{Bin: bin}); err != nil {
		t.Fatalf("ReadTrustStatus: %v", err)
	}
	args, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("reading captured args: %v", err)
	}
	if got := string(args); got != "trust\n--status\n--json\n" {
		t.Errorf("args = %q, want %q", got, "trust\n--status\n--json\n")
	}
}

// TestReadTrustStatusUsesCwd checks that the probe inspects the directory the
// REPL will run prompts in, not the REPL process's own directory.
func TestReadTrustStatusUsesCwd(t *testing.T) {
	dir := t.TempDir()
	pwdPath := filepath.Join(t.TempDir(), "pwd")
	t.Setenv("PWD_FILE", pwdPath)
	bin := writeFakeCake(t, `
printf '%s' "$PWD" > "$PWD_FILE"
echo '{"schema_version":1,"data":{"state":"trusted"}}'
exit 0
`)
	if _, err := readTrustStatus(t, TrustOptions{Bin: bin, Cwd: dir}); err != nil {
		t.Fatalf("ReadTrustStatus: %v", err)
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

func TestReadTrustStatusDebugLogWritesRawOutput(t *testing.T) {
	bin := writeFakeCake(t, `
echo '{"schema_version":1,"data":{"state":"trusted"}}'
exit 0
`)
	var log bytes.Buffer
	if _, err := readTrustStatus(t, TrustOptions{Bin: bin, DebugLog: &log}); err != nil {
		t.Fatalf("ReadTrustStatus: %v", err)
	}
	if !strings.Contains(log.String(), `"schema_version"`) {
		t.Errorf("raw envelope not written to the debug log: %q", log.String())
	}
}

// TestReadTrustStatusCancellationStopsHelper: canceling the probe context stops
// a helper that would otherwise keep running.
func TestReadTrustStatusCancellationStopsHelper(t *testing.T) {
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
		_, err := ReadTrustStatus(ctx, TrustOptions{Bin: bin})
		done <- err
	}()
	waitForFile(t, started)

	start := time.Now()
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Error("a canceled probe should return an error")
		}
		if elapsed := time.Since(start); elapsed > 10*time.Second {
			t.Errorf("canceled probe took %s, want it to stop promptly", elapsed)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("canceled probe never returned")
	}
}

// TestReadTrustStatusEnforcesStdoutCap: a pathological report fails instead of
// buffering without bound.
func TestReadTrustStatusEnforcesStdoutCap(t *testing.T) {
	bin := writeFakeCake(t, `
dd if=/dev/zero bs=1048576 count=2 2>/dev/null
exit 0
`)
	_, err := ReadTrustStatus(context.Background(), TrustOptions{Bin: bin})
	if err == nil {
		t.Fatal("output over the cap should be an error")
	}
	if !strings.Contains(err.Error(), "exceeded") {
		t.Errorf("error = %q, want it to name the output cap", err)
	}
}

// TestReadTrustStatusBoundsDescendantHoldingStdout: a helper that exits but
// leaves a descendant holding the inherited stdout pipe must not hang the
// probe. The short WaitDelay closes the pipe and surfaces the bounded teardown
// as a non-fatal error.
func TestReadTrustStatusBoundsDescendantHoldingStdout(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "pid")
	t.Setenv("PID_FILE", pidFile)
	bin := writeFakeCake(t, `
sleep 30 &
echo $! > "$PID_FILE"
echo '{"schema_version":1,"data":{"state":"trusted"}}'
exit 0
`)
	t.Cleanup(func() { killPIDFile(t, pidFile) })

	start := time.Now()
	_, err := ReadTrustStatus(context.Background(), TrustOptions{
		Bin:       bin,
		waitDelay: 200 * time.Millisecond,
	})
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("probe waited %s for a descendant-held pipe, want a bounded return", elapsed)
	}
	if err == nil {
		t.Error("a descendant-held pipe should surface as a non-fatal probe error")
	}
}
