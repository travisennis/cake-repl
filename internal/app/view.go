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
	body := m.timeline.View()
	if m.browserOpen {
		body = m.browserView()
	}
	composer := ui.PromptComposer(
		m.theme,
		m.input.View(),
		m.width,
		m.activityState(),
		m.input.LineCount(),
		m.composerHint(),
	)
	return strings.Join([]string{
		body,
		composer,
		m.statusLine(),
	}, "\n")
}

// browserView renders the /sessions overlay in place of the timeline: the
// navigable list, or a loading/empty/error body, followed by a one-line key
// hint. It occupies the timeline's height so the composer and status line do
// not move while the browser is open, so every state is bounded to the
// timeline's width and height however long or multiline its text is.
func (m Model) browserView() string {
	bodyHeight := browserBodyHeight(m.timeline.Height)
	// fill pads a short text body to the full region and caps a long or
	// multiline one; capBody only truncates, leaving the list's own layout
	// alone. The error body is arbitrary stderr, so it must be capped, not just
	// padded: a minimum Height alone lets it overflow and displace the chrome.
	fill := lipgloss.NewStyle().Width(m.width).Height(bodyHeight).MaxWidth(m.width).MaxHeight(bodyHeight)
	capBody := lipgloss.NewStyle().MaxWidth(m.width).MaxHeight(bodyHeight)

	var body string
	switch {
	case m.browserLoading:
		body = fill.Render(m.theme.Debug.Render("loading sessions…"))
	case m.browserErr != "":
		body = fill.Render(m.theme.Warning.Render("sessions unavailable: " + ui.Sanitize(m.browserErr)))
	case len(m.sessionList.Items()) == 0:
		body = fill.Render(m.theme.Info.Render("no sessions in " + ui.Sanitize(filepath.Base(m.cfg.Cwd))))
	default:
		body = capBody.Render(m.sessionList.View())
	}
	if m.timeline.Height <= 1 {
		return body
	}
	hint := m.theme.PromptHint.MaxWidth(m.width).MaxHeight(1).Render("↑/↓ move · enter resume · esc close")
	return body + "\n" + hint
}

// activityState names the state shown by the composer: a live task, the
// between-turn compactor with its whole-second age, or idle. The status line
// uses "idle" instead of "ready", but the two surfaces must never disagree
// about which operation is in flight or how long it has been running.
func (m Model) activityState() string {
	switch {
	case m.running:
		return "running"
	case m.compacting:
		return m.compactionStateLabel()
	default:
		return "ready"
	}
}

// compactionStateLabel names the in-flight compactor and its whole-second age.
// The status line and composer share it so the two surfaces cannot disagree.
func (m Model) compactionStateLabel() string {
	return "compacting " + compactionElapsedText(m.compactionElapsed)
}

// composerHint names the actions that still work in the current state. While
// the compactor runs the prompt gate is closed, so the footer advertises the two
// keys that do something instead of the submit hint that would be refused.
func (m Model) composerHint() string {
	if m.compacting {
		return "Ctrl+C cancel · Ctrl+N new session"
	}
	return "Ctrl+S submit · Enter newline · /help"
}

func (m Model) statusLine() string {
	session := "–"
	if m.session.SessionID != "" {
		session = ui.ShortID(m.session.SessionID)
	}

	state := "idle"
	switch {
	case m.running:
		state = m.spin.View() + " running"
	case m.compacting:
		state = m.spin.View() + " " + m.compactionStateLabel()
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
// reported for the current session once received, otherwise the CLI/config
// value. See ADR 014.
func (m Model) displayModel() string {
	if m.session.Model != "" {
		return m.session.Model
	}
	return m.cfg.Model
}
