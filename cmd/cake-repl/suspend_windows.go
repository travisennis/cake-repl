//go:build windows

package main

func watchSuspend(_ terminalProgram, _ bool, _ engineRuns, _ func(string, ...any)) func() {
	return func() {}
}
