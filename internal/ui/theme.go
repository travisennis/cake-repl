// Package ui renders cake-repl timeline items and chrome with lipgloss.
package ui

import "github.com/charmbracelet/lipgloss"

// Theme bundles every style the REPL renders with.
type Theme struct {
	UserLabel  lipgloss.Style
	UserText   lipgloss.Style
	Assistant  lipgloss.Style
	Reasoning  lipgloss.Style
	ToolHeader lipgloss.Style
	ToolArgs   lipgloss.Style
	ToolOutput lipgloss.Style
	Hook       lipgloss.Style
	Complete   lipgloss.Style
	Error      lipgloss.Style
	Warning    lipgloss.Style
	Info       lipgloss.Style
	Debug      lipgloss.Style
	Rule       lipgloss.Style
	StatusBar  lipgloss.Style
}

// DefaultTheme returns the standard color scheme. Styling degrades to plain
// text automatically when the color profile is Ascii (see -no-color).
func DefaultTheme() Theme {
	return Theme{
		UserLabel:  lipgloss.NewStyle().Foreground(lipgloss.Color("5")).Bold(true),
		UserText:   lipgloss.NewStyle().Faint(true),
		Assistant:  lipgloss.NewStyle(),
		Reasoning:  lipgloss.NewStyle().Foreground(lipgloss.Color("3")).Faint(true).Italic(true),
		ToolHeader: lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Bold(true),
		ToolArgs:   lipgloss.NewStyle().Foreground(lipgloss.Color("6")),
		ToolOutput: lipgloss.NewStyle().Faint(true),
		Hook:       lipgloss.NewStyle().Foreground(lipgloss.Color("3")),
		Complete:   lipgloss.NewStyle().Foreground(lipgloss.Color("2")),
		Error:      lipgloss.NewStyle().Foreground(lipgloss.Color("1")),
		Warning:    lipgloss.NewStyle().Foreground(lipgloss.Color("3")),
		Info:       lipgloss.NewStyle().Foreground(lipgloss.Color("4")),
		Debug:      lipgloss.NewStyle().Faint(true),
		Rule:       lipgloss.NewStyle().Faint(true),
		StatusBar:  lipgloss.NewStyle().Reverse(true),
	}
}
