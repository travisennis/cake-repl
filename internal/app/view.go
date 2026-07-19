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
	session := "–"
	if m.session.SessionID != "" {
		session = ui.ShortID(m.session.SessionID)
	}

	state := "idle"
	if m.running {
		state = m.spin.View() + " running"
	}

	mode, resumeID := m.session.RunOptions()
	next := mode.String()
	if mode == cake.RunResume {
		next += " " + ui.ShortID(resumeID)
	}

	return ui.StatusLine(m.theme, m.width, ui.Status{
		State:   state,
		Session: session,
		Next:    next,
		Model:   m.cfg.Model,
		Cwd:     filepath.Base(m.cfg.Cwd),
	})
}
