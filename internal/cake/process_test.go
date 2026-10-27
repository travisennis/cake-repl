//go:build !windows

package cake

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// waitingFakeCake writes a fake cake that records its pid and stays alive until
// the test cancels it. The trap makes a live-run cancellation a graceful exit;
// the listing cancellation kills the group outright.
const waitingFakeCake = `
trap 'exit 0' TERM
echo $$ > "$PID_FILE"
while :; do sleep 0.05; done
`

// waitForPIDFile waits until the fake records a positive pid, so Getsid runs
// against a live process rather than racing the shell's startup.
func waitForPIDFile(t *testing.T, path string) int {
	t.Helper()
	waitForFile(t, path)
	deadline := time.Now().Add(5 * time.Second)
	for {
		raw, err := os.ReadFile(path)
		if err == nil {
			pid, convErr := strconv.Atoi(strings.TrimSpace(string(raw)))
			if convErr == nil && pid > 0 {
				return pid
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("fake cake never recorded a pid in %s", path)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// requireSessionLeader proves the child was placed in its own session. Without
// configureProcess the child inherits the test process's session, so sid != pid.
func requireSessionLeader(t *testing.T, pid int) {
	t.Helper()
	sid, err := syscall.Getsid(pid)
	if err != nil {
		t.Fatalf("Getsid(%d): %v", pid, err)
	}
	if sid != pid {
		t.Fatalf("session id = %d, want %d (child was not detached)", sid, pid)
	}
}

func TestStartDetachesSession(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "pid")
	t.Setenv("PID_FILE", pidFile)
	t.Cleanup(func() { killPIDFile(t, pidFile) })
	bin := writeFakeCake(t, waitingFakeCake)

	run, err := Start(Options{Bin: bin, Prompt: "hi"})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	requireSessionLeader(t, waitForPIDFile(t, pidFile))

	run.Cancel()
	_, res := collectWithin(t, run, 5*time.Second)
	if !res.Canceled {
		t.Errorf("result = %+v, want Canceled", res)
	}
}

func TestReplayDetachesSession(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "pid")
	t.Setenv("PID_FILE", pidFile)
	t.Cleanup(func() { killPIDFile(t, pidFile) })
	bin := writeFakeCake(t, waitingFakeCake)

	run, err := Replay(ReplayOptions{
		Bin:       bin,
		SessionID: "11111111-2222-3333-4444-555555555555",
	})
	if err != nil {
		t.Fatalf("Replay: %v", err)
	}
	requireSessionLeader(t, waitForPIDFile(t, pidFile))

	run.Cancel()
	_, res := collectWithin(t, run, 5*time.Second)
	if !res.Canceled {
		t.Errorf("result = %+v, want Canceled", res)
	}
}

func TestListSessionsDetachesSession(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "pid")
	t.Setenv("PID_FILE", pidFile)
	t.Cleanup(func() { killPIDFile(t, pidFile) })
	bin := writeFakeCake(t, waitingFakeCake)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := ListSessions(ctx, SessionsOptions{Bin: bin})
		done <- err
	}()

	requireSessionLeader(t, waitForPIDFile(t, pidFile))
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Error("a canceled listing should return an error")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("canceled listing never returned")
	}
}
