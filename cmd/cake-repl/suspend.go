//go:build !windows

package main

import (
	"os"
	"os/signal"
	"syscall"

	"github.com/charmbracelet/x/ansi"
)

// suspendCycle performs one catchable SIGTSTP suspend/resume cycle. Every step
// is injected so the ordering contract is unit-testable without a terminal or a
// real signal.
//
// Order is the contract. The terminal is released before the process stops, or
// the shell resumes a program that still believes it owns raw mode; the modes
// Bubble Tea does not restore are written back while its renderer is still
// stopped; and the terminal is re-acquired last.
type suspendCycle struct {
	release func() error // give the terminal back before stopping
	park    func()       // anchor the live region for the resume repaint
	stop    func()       // stop the process group; returns when it is continued
	unpark  func()       // return to the buffer that owns the live region
	modes   func()       // re-enable the modes RestoreTerminal leaves off
	restore func() error // re-own the terminal
	logf    func(string, ...any)
}

// run performs one cycle. It reports whether the process stopped: when the
// terminal cannot be released the cycle is abandoned rather than stopping with
// raw mode still enabled, which is the defect this exists to fix.
func (c suspendCycle) run() bool {
	if err := c.release(); err != nil {
		c.logf("suspend: releasing the terminal failed: %v; leaving the REPL running", err)
		return false
	}
	c.park()
	c.stop() // returns after SIGCONT
	c.unpark()
	// Write the missing modes before RestoreTerminal restarts the renderer, so
	// no frame can be written between our two sequences and half a mode.
	c.modes()
	if err := c.restore(); err != nil {
		c.logf("suspend: restoring the terminal failed: %v", err)
	}
	return true
}

// watchSuspend installs the catchable-SIGTSTP handler and runs one suspendCycle
// per signal. inline selects the anchoring the resume repaint needs when the
// REPL renders outside the alternate screen. The returned function removes the
// handler; call it once the program has returned.
func watchSuspend(prog terminalProgram, inline bool, logf func(string, ...any)) func() {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGTSTP)
	done := make(chan struct{})

	go func() {
		for {
			select {
			case <-done:
				return
			case <-signals:
			}
			suspendCycle{
				release: prog.ReleaseTerminal,
				park:    func() { writeTerminal(parkSequence(inline)) },
				// Stop the whole process group outright. SIGTSTP cannot be
				// re-raised here to do it: once this program has taken SIGTSTP
				// with signal.Notify, the runtime keeps the signal blocked and
				// a Reset-and-raise is swallowed instead of stopping the
				// process. SIGSTOP cannot be caught, ignored, or blocked, so
				// the stop is reliable and fg resumes it with SIGCONT, which
				// this call returns on. The group is signalled, not just this
				// process, so the cake child pauses with the REPL instead of
				// running on unwatched.
				stop: func() {
					err := syscall.Kill(0, syscall.SIGSTOP)
					if err != nil {
						logf("suspend: stopping the process group failed: %v; resuming", err)
					}
				},
				unpark:  func() { writeTerminal(leaveParkSequence(inline)) },
				modes:   func() { writeTerminal(mouseSequence()) },
				restore: prog.RestoreTerminal,
				logf:    logf,
			}.run()
		}
	}()

	return func() {
		signal.Stop(signals)
		close(done)
	}
}

// parkSequence returns the sequence that anchors the live region for the resume
// repaint. The alternate screen is the default render mode, so releasing the
// terminal already puts the shell back on its own screen with the live region
// and cursor restored. Inline mode has none: the shell would write its
// job-control notice into the live region's rows, and the renderer's next frame
// — which moves the cursor up by the previous frame's height — would then land
// as many rows high as the shell wrote, or be drawn over stale ones. Parking the
// suspension on the alternate screen gives the shell a clean full window and
// restores the inline region's rows and cursor exactly where they were, so the
// resume repaint rewrites the region in place.
func parkSequence(inline bool) string {
	if !inline {
		return ""
	}
	return ansi.SetMode(ansi.ModeAltScreenSaveCursor)
}

// leaveParkSequence returns the terminal to the buffer parkSequence left, so the
// resume repaint happens on the screen that owns the live region.
func leaveParkSequence(inline bool) string {
	if !inline {
		return ""
	}
	return ansi.ResetMode(ansi.ModeAltScreenSaveCursor)
}

// mouseSequence re-enables the mouse reporting the REPL starts with. Bubble Tea
// releases the mouse when it gives the terminal back, but RestoreTerminal
// restores only bracketed paste and focus reporting, so without this the wheel
// stops scrolling the timeline and no mouse report reaches the program again.
func mouseSequence() string {
	return ansi.SetMode(ansi.ModeMouseButtonEvent) + ansi.SetMode(ansi.ModeMouseExtSgr)
}

// writeTerminal writes a terminal control sequence to stdout, but only when
// stdout is a character device, so a redirected run never receives one. An empty
// sequence writes nothing.
func writeTerminal(seq string) {
	if seq == "" || !stdoutIsTerminal() {
		return
	}
	_, _ = os.Stdout.WriteString(seq)
}
