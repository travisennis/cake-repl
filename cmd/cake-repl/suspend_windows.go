//go:build windows

package main

// watchSuspend is a no-op on Windows, which has no SIGTSTP and no shell job
// control to recover from. It keeps the POSIX signature so main needs no build
// tag. terminalProgram is declared once in main.go.
func watchSuspend(_ terminalProgram, _ bool, _ func(string, ...any)) func() {
	return func() {}
}
