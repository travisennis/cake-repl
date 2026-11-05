package cake

import "testing"

func TestProcessPhaseAdvancesInOrder(t *testing.T) {
	run := &Run{}
	if got := run.currentPhase(); got != processStarting {
		t.Fatalf("initial phase = %d, want starting", got)
	}
	if run.currentPhase().cancelRequested() {
		t.Error("starting phase is already cancel-requested")
	}

	run.markRunning()
	if got := run.currentPhase(); got != processRunning {
		t.Fatalf("started phase = %d, want running", got)
	}

	if !run.beginCancel() {
		t.Fatal("beginCancel did not own the transition from running")
	}
	if got := run.currentPhase(); got != processCancelRequested {
		t.Fatalf("cancel phase = %d, want cancel-requested", got)
	}
	if !run.currentPhase().cancelRequested() {
		t.Error("cancel-requested phase lost its cancellation history")
	}

	if !run.armGrace() {
		t.Fatal("armGrace did not own the transition from cancel-requested")
	}
	if got := run.currentPhase(); got != processGraceArmed {
		t.Fatalf("grace phase = %d, want grace-armed", got)
	}

	if !run.markGroupKilled() {
		t.Fatal("markGroupKilled did not own the transition from grace-armed")
	}
	if got := run.currentPhase(); got != processGroupKilled {
		t.Fatalf("kill phase = %d, want group-killed", got)
	}
	if !run.currentPhase().cancelRequested() {
		t.Error("group-killed phase lost its cancellation history")
	}

	run.markReaped()
	if got := run.currentPhase(); got != processReaped {
		t.Fatalf("reaped phase = %d, want reaped", got)
	}
	if run.currentPhase().cancelRequested() {
		t.Error("reaped phase retained cancellation history")
	}
}

func TestProcessPhaseRejectsInvalidTransitions(t *testing.T) {
	starting := &Run{}
	if starting.armGrace() {
		t.Error("armed grace from the starting phase")
	}
	if starting.markGroupKilled() {
		t.Error("recorded a group kill from the starting phase")
	}
	if got := starting.currentPhase(); got != processStarting {
		t.Fatalf("invalid transitions advanced the phase to %d", got)
	}

	running := &Run{}
	running.markRunning()
	if running.armGrace() {
		t.Error("armed grace without cancel-requested")
	}
	if running.markGroupKilled() {
		t.Error("recorded a group kill without grace-armed")
	}

	canceled := &Run{}
	if !canceled.beginCancel() {
		t.Fatal("beginCancel did not own the transition from starting")
	}
	canceled.markRunning()
	if got := canceled.currentPhase(); got != processCancelRequested {
		t.Fatalf("markRunning overwrote the cancel phase: got %d", got)
	}
	if canceled.beginCancel() {
		t.Error("a duplicate beginCancel re-owned the transition")
	}
	if !canceled.armGrace() {
		t.Fatal("armGrace did not own the transition")
	}
	if canceled.armGrace() {
		t.Error("a duplicate armGrace re-owned the transition")
	}
	if !canceled.markGroupKilled() {
		t.Fatal("markGroupKilled did not own the transition")
	}
	if canceled.markGroupKilled() {
		t.Error("a duplicate markGroupKilled re-owned the transition")
	}

	canceled.markReaped()
	if canceled.beginCancel() || canceled.armGrace() || canceled.markGroupKilled() {
		t.Error("a terminal reaped phase accepted a later transition")
	}
}

func TestProcessPhaseCancellationHistory(t *testing.T) {
	for _, tt := range []struct {
		name  string
		phase processPhase
		want  bool
	}{
		{name: "starting", phase: processStarting},
		{name: "running", phase: processRunning},
		{name: "cancel-requested", phase: processCancelRequested, want: true},
		{name: "grace-armed", phase: processGraceArmed, want: true},
		{name: "group-killed", phase: processGroupKilled, want: true},
		{name: "reaped", phase: processReaped},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.phase.cancelRequested(); got != tt.want {
				t.Errorf("cancelRequested(%d) = %v, want %v", tt.phase, got, tt.want)
			}
		})
	}
}
