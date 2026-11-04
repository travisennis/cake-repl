package childproc

import (
	"strings"
	"testing"
	"time"
)

func TestRegistryShutdownCancelsAndWaits(t *testing.T) {
	reg := NewRegistry()
	canceled := make(chan struct{})
	done := make(chan struct{})
	reg.Register("child", func() { close(canceled) }, done)

	result := make(chan error, 1)
	go func() { result <- reg.Shutdown(time.Second) }()
	<-canceled
	select {
	case err := <-result:
		t.Fatalf("Shutdown returned before the child was reaped: %v", err)
	case <-time.After(20 * time.Millisecond):
	}

	close(done)
	if err := <-result; err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
}

func TestRegistryShutdownNamesTimedOutChildren(t *testing.T) {
	never := make(chan struct{})
	reg := NewRegistry()
	reg.Register("run", func() {}, never)
	reg.Register("replay", func() {}, never)

	err := reg.Shutdown(20 * time.Millisecond)
	if err == nil {
		t.Fatal("Shutdown returned nil for children that never reaped")
	}
	for _, want := range []string{"run", "replay"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("timeout error %q does not name %q", err, want)
		}
	}
}

func TestRegistryRegisterAfterShutdownCancels(t *testing.T) {
	reg := NewRegistry()
	if err := reg.Shutdown(time.Second); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	canceled := false
	done := make(chan struct{})
	reg.Register("late", func() {
		canceled = true
		close(done)
	}, done)
	if !canceled {
		t.Fatal("Register did not cancel a child registered after Shutdown")
	}
	if got := reg.Len(); got != 0 {
		t.Fatalf("registry tracks %d children after a rejected registration, want 0", got)
	}
}

// TestRegistryShutdownWaitsForEveryOperationWithARepeatedName covers the next
// turn starting while the previous child of the same kind is still tracked: a
// reaped channel must not stand in for a live child that shares its name.
func TestRegistryShutdownWaitsForEveryOperationWithARepeatedName(t *testing.T) {
	reaped := make(chan struct{})
	live := make(chan struct{})
	reg := NewRegistry()
	reg.Register("run", func() {}, reaped)
	close(reaped)
	reg.Register("run", func() {}, live)

	err := reg.Shutdown(20 * time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "run") {
		t.Fatalf("Shutdown = %v, want a timeout naming run", err)
	}
}

func TestRegistryRemovesReapedOperation(t *testing.T) {
	reg := NewRegistry()
	done := make(chan struct{})
	reg.Register("child", func() {}, done)
	if got := reg.Len(); got != 1 {
		t.Fatalf("registry tracks %d children, want 1", got)
	}

	close(done)
	deadline := time.Now().Add(time.Second)
	for reg.Len() != 0 {
		if time.Now().After(deadline) {
			t.Fatal("reaped child was not removed from the registry")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestNilRegistryIsSafe(t *testing.T) {
	var reg *Registry
	reg.Register("child", func() {}, make(chan struct{}))
	if got := reg.Len(); got != 0 {
		t.Fatalf("nil registry Len = %d, want 0", got)
	}
	if err := reg.Shutdown(time.Second); err != nil {
		t.Fatalf("nil registry Shutdown: %v", err)
	}
}
