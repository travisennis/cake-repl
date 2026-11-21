//go:build windows

package main

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/travisennis/cake-repl/internal/cake"
)

func watchSuspend(_ *tea.Program, _ bool, _ *cake.Registry, _ func(string, ...any)) func() {
	return func() {}
}
