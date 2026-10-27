package cake

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// TestRegistryToleratesEmptyAndReaped: an empty registry, a zero-valued Run, and
// a run already reaped by cmd.Wait must all be no-ops. The registry's real
// signal behavior is covered by the terminal milestone; this pins the safety
// guards that keep a suspend from signaling pid 0 or a finished process.
func TestRegistryToleratesEmptyAndReaped(t *testing.T) {
	var logged []string
	reg := NewRegistry(func(format string, args ...any) {
		logged = append(logged, fmt.Sprintf(format, args...))
	})

	reg.StopAll()
	reg.ContinueAll()

	reaped := &Run{pid: 12345}
	reaped.exited.Store(true)
	reg.Add(reaped)
	reg.StopAll()
	reg.ContinueAll()

	zero := &Run{}
	reg.Add(zero)
	reg.StopAll()
	reg.ContinueAll()

	reg.Remove(reaped)
	reg.Remove(zero)
	if len(logged) != 0 {
		t.Errorf("safe no-ops logged failures: %v", logged)
	}
}

// TestNilRegistryIsSafe keeps a caller that never wires a registry from
// panicking during startup or shutdown.
func TestNilRegistryIsSafe(t *testing.T) {
	var reg *Registry
	reg.Add(&Run{pid: 12345})
	reg.Remove(&Run{pid: 12345})
	reg.StopAll()
	reg.ContinueAll()

	NewRegistry(nil).StopAll()
}

// TestStartRegistersRunForItsLifetime pins the registration window: a run is
// present from a successful start until cmd.Wait returns, so a suspend can
// reach it and a later suspend cannot signal a reaped pid.
func TestStartRegistersRunForItsLifetime(t *testing.T) {
	started := filepath.Join(t.TempDir(), "started")
	stop := filepath.Join(t.TempDir(), "stop")
	t.Setenv("STARTED_FILE", started)
	t.Setenv("STOP_FILE", stop)
	bin := writeFakeCake(t, `
touch "$STARTED_FILE"
while [ ! -f "$STOP_FILE" ]; do sleep 0.01; done
`)

	reg := NewRegistry(nil)
	run, err := Start(Options{Bin: bin, Prompt: "hi", Registry: reg})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(run.Cancel)
	waitForFile(t, started)
	if got := reg.snapshot(); len(got) != 1 || got[0] != run {
		t.Fatalf("registry = %v, want the live run", got)
	}

	if err := os.WriteFile(stop, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	_, res := collect(t, run)
	if res.ExitCode != 0 || res.Canceled || res.Err != nil {
		t.Fatalf("result = %+v, want a clean exit", res)
	}
	if got := reg.snapshot(); len(got) != 0 {
		t.Fatalf("registry = %v after reaping, want empty", got)
	}
}
