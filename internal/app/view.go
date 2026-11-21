package app

import (
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/travisennis/cake-repl/internal/cake"
	"github.com/travisennis/cake-repl/internal/ui"
)

func (m Model) View() string {
	if !m.ready {
		return "starting cake-repl…"
	}
	p := m.presentationState()
	body := m.viewport.View()
	if p.browser {
		body = m.browserView(p)
	}
	composer := ui.PromptComposer(
		m.theme,
		m.input.View(),
		m.width,
		p.activityLabel(),
		m.input.LineCount(),
		p.hint,
	)
	return strings.Join([]string{
		body,
		composer,
		m.statusLineFor(p),
	}, "\n")
}

func (m Model) browserView(p presentation) string {
	bodyHeight := browserBodyHeight(m.viewport.Height)
	fill := lipgloss.NewStyle().Width(m.width).Height(bodyHeight).MaxWidth(m.width).MaxHeight(bodyHeight)
	capBody := lipgloss.NewStyle().MaxWidth(m.width).MaxHeight(bodyHeight)

	var body string
	switch m.browser.phase {
	case browserLoading:
		body = fill.Render(m.theme.Debug.Render("loading sessions…"))
	case browserError:
		body = fill.Render(m.theme.Warning.Render("sessions unavailable: " + ui.Sanitize(m.browser.err)))
	case browserEmpty:
		body = fill.Render(m.theme.Info.Render("no sessions in " + ui.Sanitize(filepath.Base(m.cfg.Cwd))))
	case browserReady:
		body = capBody.Render(m.sessionList.View())
	}
	if m.viewport.Height <= 1 {
		return body
	}
	hint := m.theme.PromptHint.MaxWidth(m.width).MaxHeight(1).Render(p.hint)
	return body + "\n" + hint
}

func (m Model) statusLineFor(p presentation) string {
	session := "–"
	if m.session.SessionID != "" {
		session = ui.ShortID(m.session.SessionID)
	}

	state := p.label
	if p.background != "" {
		state += " · " + p.background
	}
	if p.activity == activityRunning || p.activity == activityCompacting {
		state = m.spin.View() + " " + state
	}

	mode, resumeID := m.session.RunOptions()
	next := mode.String()
	if mode == cake.RunResume {
		next += " " + ui.ShortID(resumeID)
	}

	context := ""
	if c := m.session.Context; c != nil {
		context = ui.ContextValue(c.Tokens, c.Window, c.Estimated)
	}

	return ui.StatusLine(m.theme, m.width, ui.Status{
		State:   state,
		Session: session,
		Next:    next,
		Model:   m.displayModel(),
		Context: context,
		Cwd:     filepath.Base(m.cfg.Cwd),
	})
}

func (m Model) displayModel() string {
	if m.session.Model != "" {
		return m.session.Model
	}
	return m.turn.Model
}
