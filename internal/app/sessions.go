package app

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
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

// resetSessionList drops every row and resets the cursor. It runs when a load
// starts and when one fails so the list only ever holds rows from the current
// successful listing; otherwise a stale item from a previous visit would stay
// selectable behind the loading or error body the user is looking at.
func (m *Model) resetSessionList() {
	m.sessionList.SetItems(nil)
	m.sessionList.ResetSelected()
}

// openSessionBrowser shows the idle session browser and starts loading this
// directory's sessions. `/sessions` and the startup picker a bare `-resume`
// requests both come through here, so the two openings present the same title,
// loading state, and reload: the previous listing is dropped before the reload
// starts, so no stale row is ever selectable behind the loading body (ADR 019).
// It returns the updated model and the listing command as a pair, so a caller
// never reads a receiver mutated in a sibling return operand.
func (m Model) openSessionBrowser() (Model, tea.Cmd) {
	m.clearSuggestion()
	m.browserOpen = true
	m.browserLoading = true
	m.browserErr = ""
	m.resetSessionList()
	m.sessionList.Title = "sessions in " + ui.Sanitize(filepath.Base(m.cfg.Cwd))
	return m.startSessionListLoad()
}

// closeBrowser hides the /sessions overlay and cancels any in-flight listing.
// The helper the browser spawned is model-owned, so closing the browser is the
// point it must be told to stop; a result already on its way is invalidated and
// dropped when it arrives. The composer and its draft are untouched, so a prompt
// stays available after closing.
func (m *Model) closeBrowser() {
	m.browserOpen = false
	m.browserLoading = false
	m.cancelSessionList()
}

// cancelSessionList stops the in-flight listing, if any, and invalidates its
// request identity so a result already in flight is ignored. Identities start at
// 1, so 0 is never live; it is safe to call with no listing active.
func (m *Model) cancelSessionList() {
	if m.sessionListCancel != nil {
		m.sessionListCancel()
		m.sessionListCancel = nil
	}
	m.sessionListReq = 0
}

// browserSelectable reports whether the browser is showing a list with a row
// the user can act on. It is the precondition for selection: Enter is a no-op
// while a load is in flight, after a failed load, or on an empty listing, so it
// can only ever resume a row visible on the current screen.
//
// resetSessionList clears rows both when a load starts and when one fails, so
// the loading and error clauses are redundant with len(items) > 0 today. They
// stay so the guard does not silently become "any row on screen" if a future
// change lets a superseded or failed request leave rows behind: dropping them
// would then let Enter resume a row the user cannot see.
func (m *Model) browserSelectable() bool {
	return !m.browserLoading && m.browserErr == "" && len(m.sessionList.Items()) > 0
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
