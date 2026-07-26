// Package ui renders cake-repl timeline items and chrome with lipgloss.
package ui

import "github.com/charmbracelet/lipgloss"

// Theme bundles every style the REPL renders with.
type Theme struct {
	// Timeline content roles.
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

	// Prompt composer.
	PromptTitle  lipgloss.Style
	PromptBorder lipgloss.Style
	PromptHint   lipgloss.Style
	Placeholder  lipgloss.Style

	// Status bar.
	StatusBar       lipgloss.Style
	StatusState     lipgloss.Style
	StatusKey       lipgloss.Style
	StatusValue     lipgloss.Style
	StatusSeparator lipgloss.Style

	// Shared metadata and timeline structure.
	TimelineSeparator lipgloss.Style
	TimelineAccent    lipgloss.Style
}

// DefaultTheme returns the standard color scheme. Styling degrades to plain
// text automatically when the color profile is Ascii (see -no-color).
func DefaultTheme() Theme {
	return Theme{
		UserLabel:         lipgloss.NewStyle().Foreground(lipgloss.Color("5")).Bold(true),
		UserText:          lipgloss.NewStyle().Bold(true),
		Assistant:         lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Bold(true),
		Reasoning:         lipgloss.NewStyle().Foreground(lipgloss.Color("3")).Faint(true).Italic(true),
		ToolHeader:        lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Bold(true),
		ToolArgs:          lipgloss.NewStyle().Foreground(lipgloss.Color("6")),
		ToolOutput:        lipgloss.NewStyle().Faint(true),
		Hook:              lipgloss.NewStyle().Foreground(lipgloss.Color("3")),
		Complete:          lipgloss.NewStyle().Foreground(lipgloss.Color("2")),
		Error:             lipgloss.NewStyle().Foreground(lipgloss.Color("1")),
		Warning:           lipgloss.NewStyle().Foreground(lipgloss.Color("3")),
		Info:              lipgloss.NewStyle().Foreground(lipgloss.Color("4")),
		Debug:             lipgloss.NewStyle().Faint(true),
		PromptTitle:       lipgloss.NewStyle().Bold(true),
		PromptBorder:      lipgloss.NewStyle().Faint(true),
		PromptHint:        lipgloss.NewStyle().Faint(true),
		Placeholder:       lipgloss.NewStyle().Faint(true),
		StatusBar:         lipgloss.NewStyle().Background(lipgloss.Color("236")).Foreground(lipgloss.Color("252")),
		StatusState:       lipgloss.NewStyle().Background(lipgloss.Color("2")).Foreground(lipgloss.Color("15")).Bold(true),
		StatusKey:         lipgloss.NewStyle().Foreground(lipgloss.Color("81")).Bold(true),
		StatusValue:       lipgloss.NewStyle().Foreground(lipgloss.Color("252")),
		StatusSeparator:   lipgloss.NewStyle().Foreground(lipgloss.Color("243")),
		TimelineSeparator: lipgloss.NewStyle().Faint(true),
		TimelineAccent:    lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Faint(true),
	}
}
