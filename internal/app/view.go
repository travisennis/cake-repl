package app

import (
	"path/filepath"
	"strings"

	"github.com/travisennis/cake-repl/internal/cake"
	"github.com/travisennis/cake-repl/internal/ui"
)

// View implements tea.Model.
func (m Model) View() string {
	if !m.ready {
		return "starting cake-repl…"
	}
	composer := ui.PromptComposer(
		m.theme,
		m.input.View(),
		m.width,
		m.running,
		m.input.LineCount(),
		"Ctrl+S submit · Enter newline · /help",
	)
	return strings.Join([]string{
		m.timeline.View(),
		composer,
		m.statusLine(),
	}, "\n")
}

func (m Model) statusLine() string {
	session := "session –"
	if m.session.SessionID != "" {
		session = "session " + ui.ShortID(m.session.SessionID)
	}

	state := "idle"
	if m.running {
		state = m.spin.View() + " running"
	}

	mode, resumeID := m.session.RunOptions()
	next := "next: " + mode.String()
	if mode == cake.RunResume {
		next += " " + ui.ShortID(resumeID)
	}

	model := ""
	if m.cfg.Model != "" {
		model = "model " + m.cfg.Model
	}

	return ui.StatusLine(m.theme, m.width, session, state, next, model, filepath.Base(m.cfg.Cwd))
}
