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
// successful listing; otherwise rows from a previous visit could survive into
// the next ready body after a reload or failure.
func (m *Model) resetSessionList() {
	m.sessionList.SetItems(nil)
	m.sessionList.ResetSelected()
}

// browserPhase is the presentation state of the /sessions overlay. It is not
// the listing request's lifecycle: request identity, cancellation, and draining
// stay in the model's sessionListReq, sessionListCancel, and sessionListDone
// fields, because a replacement can leave more than one canceled listing
// draining. The phase only answers what the browser is showing and therefore
// whether a row is selectable.
type browserPhase uint8

const (
	browserClosed browserPhase = iota
	browserLoading
	browserReady
	browserEmpty
	browserError
)

// sessionBrowser is the one source for the overlay's visibility, selectability,
// and body. The transition methods keep a ready phase paired with at least one
// row and an empty phase with none; selectability has no separate item-count
// check that could drift from the body.
type sessionBrowser struct {
	phase browserPhase
	err   string
}

func (b sessionBrowser) open() bool {
	return b.phase != browserClosed
}

func (b sessionBrowser) selectable() bool {
	return b.phase == browserReady
}

func (b sessionBrowser) loading() bool {
	return b.phase == browserLoading
}

// startLoading opens or reloads the overlay. It clears any previous error
// before the caller drops the old rows and starts a new request.
func (b *sessionBrowser) startLoading() {
	*b = sessionBrowser{phase: browserLoading}
}

// dismiss closes the overlay without retaining any presentation state. The
// caller cancels and invalidates the listing separately.
func (b *sessionBrowser) dismiss() {
	*b = sessionBrowser{}
}

// loaded settles a current successful listing. An empty result is its own
// phase, not a ready list with zero rows, so selectability cannot disagree with
// the body the user sees.
func (b *sessionBrowser) loaded(count int) {
	b.err = ""
	if count == 0 {
		b.phase = browserEmpty
		return
	}
	b.phase = browserReady
}

// failed settles a current listing failure. The error text is presentation owned
// by the error phase and is cleared by the next load or close.
func (b *sessionBrowser) failed(err string) {
	b.phase = browserError
	b.err = err
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
	m.browser.startLoading()
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
	m.browser.dismiss()
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
// the user can act on. It is the browser phase's single selection rule: Enter is
// a no-op while a load is in flight, after a failed load, or on an empty
// listing, so it can only ever resume a row visible on the current screen. The
// ready phase is assigned only after a current successful listing set at least
// one row, so there is no second item-count check to drift from the body.
func (m *Model) browserSelectable() bool {
	return m.browser.selectable()
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
