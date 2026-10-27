//go:build !windows

package cake

import (
	"context"
	"errors"
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
	t.Cleanup(run.Cancel)
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
	t.Cleanup(run.Cancel)
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

// waitForGone polls kill(pid, 0) until the process is reaped. A descendant
// killed with the engine group can briefly remain visible as a zombie.
func waitForGone(t *testing.T, pid int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		err := syscall.Kill(pid, 0)
		if errors.Is(err, syscall.ESRCH) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("descendant %d is still alive (kill 0: %v)", pid, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestCancelKillsDescendantGroup covers both pipe shapes: a background child
// holding stdout and stderr, and one holding only stdout. WaitDelay alone
// cannot unblock the second shape (Start reads stdout to EOF before Wait), so
// finishing at the grace window proves the group SIGKILL did the work.
func TestCancelKillsDescendantGroup(t *testing.T) {
	for _, tc := range []struct {
		name     string
		redirect string
	}{
		{name: "stdout and stderr"},
		{name: "stdout only", redirect: "2>/dev/null"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			descFile := filepath.Join(t.TempDir(), "desc")
			t.Setenv("DESC_FILE", descFile)
			t.Cleanup(func() { killPIDFile(t, descFile) })
			bin := writeFakeCake(t, `
trap '' TERM
sleep 30 `+tc.redirect+` &
echo $! > "$DESC_FILE"
while :; do sleep 0.05; done
`)

			run, err := Start(Options{
				Bin:       bin,
				Prompt:    "hi",
				waitDelay: 100 * time.Millisecond,
			})
			if err != nil {
				t.Fatalf("Start: %v", err)
			}
			t.Cleanup(run.Cancel)
			desc := waitForPIDFile(t, descFile)

			start := time.Now()
			run.Cancel()
			_, res := collectWithin(t, run, 5*time.Second)
			elapsed := time.Since(start)

			if !res.Canceled {
				t.Errorf("result = %+v, want Canceled", res)
			}
			if res.Err != nil {
				t.Errorf("group kill should not be an error: %v", res.Err)
			}
			if elapsed >= time.Second {
				t.Errorf("cancellation took %s; want the grace-window group kill, not the later WaitDelay", elapsed)
			}
			waitForGone(t, desc, 2*time.Second)
		})
	}
}
