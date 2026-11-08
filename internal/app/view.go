package app

import (
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/travisennis/cake-repl/internal/cake"
	"github.com/travisennis/cake-repl/internal/ui"
)

// View implements tea.Model.
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

// browserView renders the /sessions overlay in place of the timeline: the
// navigable list, or a loading/empty/error body, followed by a one-line key
// hint. It occupies the timeline's height so the composer and status line do
// not move while the browser is open, so every state is bounded to the
// timeline's width and height however long or multiline its text is.
func (m Model) browserView(p presentation) string {
	bodyHeight := browserBodyHeight(m.viewport.Height)
	// fill pads a short text body to the full region and caps a long or
	// multiline one; capBody only truncates, leaving the list's own layout
	// alone. The error body is arbitrary stderr, so it must be capped, not just
	// padded: a minimum Height alone lets it overflow and displace the chrome.
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

// activityState is the composer activity label for callers that do not already
// hold the presentation value, such as focused tests.
func (m Model) activityState() string {
	return m.presentationState().activityLabel()
}

// composerHint is the footer hint for callers that do not already hold the
// presentation value.
func (m Model) composerHint() string {
	return m.presentationState().hint
}

func (m Model) statusLine() string {
	return m.statusLineFor(m.presentationState())
}

func (m Model) statusLineFor(p presentation) string {
	session := "–"
	if m.session.SessionID != "" {
		session = ui.ShortID(m.session.SessionID)
	}

	state := p.label
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

// displayModel returns the model shown in the status line: the identity cake
// reported for the current session once received, otherwise the current turn
// selection (initialized from the startup -model/config value). See ADR 007 and
// ADR 014.
func (m Model) displayModel() string {
	if m.session.Model != "" {
		return m.session.Model
	}
	return m.turn.Model
}
