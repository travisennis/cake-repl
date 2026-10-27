//go:build !windows

package compact

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

// waitingFakeCompactor records its pid, probes the controlling terminal, and
// stays alive until the test cancels it. The trap makes cancellation a graceful
// exit, so the run is classified as canceled rather than failed.
const waitingFakeCompactor = `
trap 'exit 0' TERM
echo $$ > "$PID_FILE"
if : < /dev/tty 2>/dev/null; then printf openable > "$TTY_FILE"; else printf blocked > "$TTY_FILE"; fi
while :; do sleep 0.05; done
`

func waitForFile(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(path); err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", path)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func waitForPID(t *testing.T, path string) int {
	t.Helper()
	waitForFile(t, path)
	deadline := time.Now().Add(5 * time.Second)
	for {
		raw, err := os.ReadFile(path)
		if err == nil {
			if pid, convErr := strconv.Atoi(strings.TrimSpace(string(raw))); convErr == nil && pid > 0 {
				return pid
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("fake compactor never recorded a pid in %s", path)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// startWaitingCompactor runs Compact in the background against a fake that
// stays alive, and returns the channels the test uses to observe and stop it.
func startWaitingCompactor(t *testing.T) (done <-chan Result, stop func()) {
	t.Helper()
	bin := writeFakeCompactor(t, waitingFakeCompactor)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	ch := make(chan Result, 1)
	go func() {
		ch <- Compact(ctx, Options{Bin: bin, SessionID: "s-1", waitDelay: 500 * time.Millisecond})
	}()
	return ch, cancel
}

// TestCompactChildIsSessionLeader proves the compactor runs on the same
// process boundary as a cake run: without childproc.Isolate it inherits the
// REPL's session, so its session id differs from its pid. See ADR 027.
func TestCompactChildIsSessionLeader(t *testing.T) {
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "pid")
	t.Setenv("PID_FILE", pidFile)
	t.Setenv("TTY_FILE", filepath.Join(dir, "tty"))

	done, stop := startWaitingCompactor(t)
	pid := waitForPID(t, pidFile)

	sid, err := syscall.Getsid(pid)
	if err != nil {
		t.Fatalf("getsid(%d): %v", pid, err)
	}
	if sid != pid {
		t.Errorf("session id = %d, want %d (compactor was not detached)", sid, pid)
	}

	stop()
	select {
	case res := <-done:
		if !res.Canceled {
			t.Fatalf("result = %+v, want Canceled", res)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Compact did not return after cancellation")
	}
}

// TestCompactChildCannotOpenControllingTerminal checks the consequence of the
// session boundary: the compactor's explicit /dev/tty open fails. Without a
// controlling terminal in the test process the probe cannot distinguish an
// isolated child from an inherited one, so skip instead of passing vacuously;
// TestCompactChildIsSessionLeader is the deterministic proof on every platform.
func TestCompactChildCannotOpenControllingTerminal(t *testing.T) {
	f, err := os.Open("/dev/tty")
	if err != nil {
		t.Skip("test process has no controlling terminal; see TestCompactChildIsSessionLeader")
	}
	_ = f.Close()

	dir := t.TempDir()
	t.Setenv("PID_FILE", filepath.Join(dir, "pid"))
	ttyFile := filepath.Join(dir, "tty")
	t.Setenv("TTY_FILE", ttyFile)

	done, stop := startWaitingCompactor(t)
	waitForFile(t, ttyFile)
	raw, err := os.ReadFile(ttyFile)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(raw)); got != "blocked" {
		t.Errorf("compactor /dev/tty probe = %q, want blocked", got)
	}

	stop()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Compact did not return after cancellation")
	}
}
