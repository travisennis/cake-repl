//go:build !windows

package main

import (
	"os"
	"os/signal"
	"syscall"

	"github.com/charmbracelet/x/ansi"
)

type suspendCycle struct {
	release      func() error
	park         func()
	stopRuns     func()
	stop         func()
	continueRuns func()
	unpark       func()
	modes        func()
	restore      func() error
	logf         func(string, ...any)
}

func (c suspendCycle) run() bool {
	if err := c.release(); err != nil {
		c.logf("suspend: releasing the terminal failed: %v; leaving the REPL running", err)
		return false
	}
	c.park()
	c.stopRuns()
	c.stop()
	c.continueRuns()
	c.unpark()
	c.modes()
	if err := c.restore(); err != nil {
		c.logf("suspend: restoring the terminal failed: %v", err)
	}
	return true
}

func watchSuspend(prog terminalProgram, inline bool, runs engineRuns, logf func(string, ...any)) func() {
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
				stopRuns: func() {
					if runs != nil {
						runs.StopAll()
					}
				},
				stop: func() {
					err := syscall.Kill(0, syscall.SIGSTOP)
					if err != nil {
						logf("suspend: stopping the process group failed: %v; resuming", err)
					}
				},
				continueRuns: func() {
					if runs != nil {
						runs.ContinueAll()
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

func parkSequence(inline bool) string {
	if !inline {
		return ""
	}
	return ansi.SetMode(ansi.ModeAltScreenSaveCursor)
}

func leaveParkSequence(inline bool) string {
	if !inline {
		return ""
	}
	return ansi.ResetMode(ansi.ModeAltScreenSaveCursor)
}

func mouseSequence() string {
	return ansi.SetMode(ansi.ModeMouseButtonEvent) + ansi.SetMode(ansi.ModeMouseExtSgr)
}

func writeTerminal(seq string) {
	if seq == "" || !stdoutIsTerminal() {
		return
	}
	_, _ = os.Stdout.WriteString(seq)
}
