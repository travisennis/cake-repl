//go:build !windows

package main

import (
	"errors"
	"strings"
	"testing"
)

// recordingCycle returns a suspendCycle whose steps append their name to a log,
// so the ordering contract can be asserted without a terminal or a real signal.
func recordingCycle(steps *[]string, releaseErr error) suspendCycle {
	record := func(name string) { *steps = append(*steps, name) }
	return suspendCycle{
		release: func() error { record("release"); return releaseErr },
		park:    func() { record("park") },
		stop:    func() { record("stop") },
		unpark:  func() { record("unpark") },
		modes:   func() { record("modes") },
		restore: func() error { record("restore"); return nil },
		logf:    func(string, ...any) {},
	}
}

// TestSuspendCycleRunsStepsInOrder pins the release/stop/restore contract: the
// terminal has to be released before the process stops, the modes Bubble Tea
// does not restore have to be written while the renderer is still stopped, and
// the terminal has to be re-acquired last.
func TestSuspendCycleRunsStepsInOrder(t *testing.T) {
	var steps []string
	if stopped := recordingCycle(&steps, nil).run(); !stopped {
		t.Fatal("run() reported the process did not stop, want it to have stopped")
	}
	want := []string{"release", "park", "stop", "unpark", "modes", "restore"}
	if strings.Join(steps, ",") != strings.Join(want, ",") {
		t.Errorf("steps = %v, want %v", steps, want)
	}
}

// TestSuspendCycleSkipsStopWhenReleaseFails pins the safe failure: stopping with
// raw mode still enabled is the defect, so a release that fails must abandon the
// cycle and leave the REPL running rather than fall back to it.
func TestSuspendCycleSkipsStopWhenReleaseFails(t *testing.T) {
	var steps []string
	cycle := recordingCycle(&steps, errors.New("release failed"))
	if stopped := cycle.run(); stopped {
		t.Error("run() reported the process stopped after a failed release, want it to stay running")
	}
	if len(steps) != 1 || steps[0] != "release" {
		t.Errorf("steps = %v, want only the failed release", steps)
	}
}

// TestSuspendCycleLogsForReleaseAndRestoreFailures keeps the failures visible in
// the debug log, since neither has a timeline surface.
func TestSuspendCycleLogsFailures(t *testing.T) {
	var logged []string
	cycle := suspendCycle{
		release: func() error { return errors.New("no terminal") },
		park:    func() {},
		stop:    func() {},
		unpark:  func() {},
		modes:   func() {},
		restore: func() error { return nil },
		logf:    func(format string, args ...any) { logged = append(logged, format) },
	}
	cycle.run()
	if len(logged) != 1 || !strings.Contains(logged[0], "releasing the terminal failed") {
		t.Errorf("logged = %v, want one release failure", logged)
	}

	logged = nil
	cycle.release = func() error { return nil }
	cycle.restore = func() error { return errors.New("restore failed") }
	cycle.run()
	if len(logged) != 1 || !strings.Contains(logged[0], "restoring the terminal failed") {
		t.Errorf("logged = %v, want one restore failure", logged)
	}
}

// TestParkSequencesAreAltScreenOnlyForInline pins the render-mode split: the
// alternate screen is the default, so only inline mode needs a park/unpark pair,
// and the pair has to be the balanced save-cursor enter and leave.
func TestParkSequencesAreAltScreenOnlyForInline(t *testing.T) {
	if got := parkSequence(false); got != "" {
		t.Errorf("parkSequence(false) = %q, want empty", got)
	}
	if got := leaveParkSequence(false); got != "" {
		t.Errorf("leaveParkSequence(false) = %q, want empty", got)
	}
	if got := parkSequence(true); got != "\x1b[?1049h" {
		t.Errorf("parkSequence(true) = %q, want the alternate-screen enter", got)
	}
	if got := leaveParkSequence(true); got != "\x1b[?1049l" {
		t.Errorf("leaveParkSequence(true) = %q, want the alternate-screen leave", got)
	}
}

// TestMouseSequenceReenablesReporting pins the mode Bubble Tea's RestoreTerminal
// does not restore. Without it the wheel stops scrolling the timeline after a
// resume.
func TestMouseSequenceReenablesReporting(t *testing.T) {
	got := mouseSequence()
	if got != "\x1b[?1002h\x1b[?1006h" {
		t.Errorf("mouseSequence() = %q, want cell-motion plus SGR reporting", got)
	}
}

// TestWatchSuspendInstallsAndRemovesHandler covers the wiring: installing the
// watcher and removing it must not panic, and removing it must give the returned
// stop function.
func TestWatchSuspendInstallsAndRemovesHandler(t *testing.T) {
	prog := &stubProgram{}
	stop := watchSuspend(prog, false, func(string, ...any) {})
	if stop == nil {
		t.Fatal("watchSuspend returned a nil stop function")
	}
	stop()
}

// stubProgram satisfies terminalProgram without a terminal.
type stubProgram struct{}

func (s *stubProgram) ReleaseTerminal() error { return nil }

func (s *stubProgram) RestoreTerminal() error { return nil }
