//go:build !windows

package main

import (
	"os"
	"os/signal"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/travisennis/cake-repl/internal/cake"
)

func suspendOnce(prog *tea.Program, inline bool, runs *cake.Registry, logf func(string, ...any)) bool {
	if err := prog.ReleaseTerminal(); err != nil {
		logf("suspend: releasing the terminal failed: %v; leaving the REPL running", err)
		return false
	}
	writeTerminal(parkSequence(inline))
	if runs != nil {
		runs.StopAll()
	}
	if err := syscall.Kill(0, syscall.SIGSTOP); err != nil {
		logf("suspend: stopping the process group failed: %v; resuming", err)
	}
	if runs != nil {
		runs.ContinueAll()
	}
	writeTerminal(leaveParkSequence(inline))
	writeTerminal(mouseSequence())
	if err := prog.RestoreTerminal(); err != nil {
		logf("suspend: restoring the terminal failed: %v", err)
	}
	return true
}

func watchSuspend(prog *tea.Program, inline bool, runs *cake.Registry, logf func(string, ...any)) func() {
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
			suspendOnce(prog, inline, runs, logf)
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
