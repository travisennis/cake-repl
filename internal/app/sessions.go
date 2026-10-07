package app

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/list"
	"github.com/travisennis/cake-repl/internal/cake"
	"github.com/travisennis/cake-repl/internal/ui"
)

// sessionItem is one row in the /sessions browser. The title is pre-rendered
// when the list is built (short id, relative age, sanitized first prompt)
// because the list delegate renders it without access to the current time. The
// item keeps the raw summary so selecting it can pin the next prompt.
type sessionItem struct {
	summary cake.SessionSummary
	title   string
}

func (i sessionItem) Title() string       { return i.title }
func (i sessionItem) Description() string { return "" }
func (i sessionItem) FilterValue() string { return i.summary.SessionID }

// newSessionItem builds one browser row. Untrusted session fields are
// sanitized and the prompt is flattened to a single line before they reach the
// list (ADR 005); the row is one line, so a newline would break its height.
func newSessionItem(s cake.SessionSummary, now time.Time) sessionItem {
	title := ui.Sanitize(ui.ShortID(s.SessionID)) + "  " + relativeTime(s.Timestamp, now)
	if prompt := strings.Join(strings.Fields(ui.Sanitize(s.FirstPrompt)), " "); prompt != "" {
		title += "  " + prompt
	}
	return sessionItem{summary: s, title: title}
}

// newSessionList constructs the idle session browser. Filtering, the status
// bar, and the help footer are off: the browser is a fixed, navigable list with
// its own key hint, not a filterable picker.
func newSessionList() list.Model {
	delegate := list.NewDefaultDelegate()
	delegate.ShowDescription = false
	m := list.New(nil, delegate, 0, 0)
	m.SetShowHelp(false)
	m.SetShowStatusBar(false)
	m.SetFilteringEnabled(false)
	return m
}

// resizeSessionList matches the browser list to the timeline viewport so the
// overlay replaces it without moving the composer or status line.
func (m *Model) resizeSessionList(viewportHeight int) {
	w, h := m.width, browserBodyHeight(viewportHeight)
	if m.sessionList.Width() == w && m.sessionList.Height() == h {
		return
	}
	m.sessionList.SetSize(w, h)
}

// browserBodyHeight returns the rows the browser list body occupies at a given
// timeline viewport height. The browser replaces the timeline and reserves one
// row for the key hint when there is room.
func browserBodyHeight(viewportHeight int) int {
	if viewportHeight > 1 {
		return viewportHeight - 1
	}
	if viewportHeight < 1 {
		return 1
	}
	return viewportHeight
}

// relativeTime renders a coarse, human-readable age for a session timestamp. A
// zero time (an unparseable timestamp) renders as an em dash.
func relativeTime(t, now time.Time) string {
	if t.IsZero() {
		return "—"
	}
	switch d := now.Sub(t); {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d < 30*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	default:
		return t.Format("2006-01-02")
	}
}
