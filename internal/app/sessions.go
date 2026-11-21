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

type sessionItem struct {
	summary cake.SessionSummary
	title   string
}

func (i sessionItem) Title() string       { return i.title }
func (i sessionItem) Description() string { return "" }
func (i sessionItem) FilterValue() string { return i.summary.SessionID }

func newSessionItem(s cake.SessionSummary, now time.Time) sessionItem {
	title := ui.Sanitize(ui.ShortID(s.SessionID)) + "  " + relativeTime(s.Timestamp, now)
	if prompt := strings.Join(strings.Fields(ui.Sanitize(s.FirstPrompt)), " "); prompt != "" {
		title += "  " + prompt
	}
	return sessionItem{summary: s, title: title}
}

func newSessionList() list.Model {
	delegate := list.NewDefaultDelegate()
	delegate.ShowDescription = false
	m := list.New(nil, delegate, 0, 0)
	m.SetShowHelp(false)
	m.SetShowStatusBar(false)
	m.SetFilteringEnabled(false)
	return m
}

func (m *Model) resizeSessionList(viewportHeight int) {
	w, h := m.frameWidth(), browserBodyHeight(viewportHeight)
	if m.sessionList.Width() == w && m.sessionList.Height() == h {
		return
	}
	m.sessionList.SetSize(w, h)
}

func (m *Model) resetSessionList() {
	m.sessionList.SetItems(nil)
	m.sessionList.ResetSelected()
}

type browserPhase uint8

const (
	browserClosed browserPhase = iota
	browserLoading
	browserReady
	browserEmpty
	browserError
)

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

func (b *sessionBrowser) startLoading() {
	*b = sessionBrowser{phase: browserLoading}
}

func (b *sessionBrowser) dismiss() {
	*b = sessionBrowser{}
}

func (b *sessionBrowser) loaded(count int) {
	b.err = ""
	if count == 0 {
		b.phase = browserEmpty
		return
	}
	b.phase = browserReady
}

func (b *sessionBrowser) failed(err string) {
	b.phase = browserError
	b.err = err
}

func (m Model) openSessionBrowser() (Model, tea.Cmd) {
	m.clearSuggestion()
	m.browser.startLoading()
	m.resetSessionList()
	m.sessionList.Title = "sessions in " + ui.Sanitize(filepath.Base(m.cfg.Cwd))
	return m.startSessionListLoad()
}

func (m *Model) closeBrowser() {
	m.browser.dismiss()
	m.cancelSessionList()
}

func (m *Model) cancelSessionList() {
	if m.sessionListCancel != nil {
		m.sessionListCancel()
		m.sessionListCancel = nil
	}
	m.sessionListReq = 0
}

func (m *Model) browserSelectable() bool {
	return m.browser.selectable()
}

func browserBodyHeight(viewportHeight int) int {
	if viewportHeight > 1 {
		return viewportHeight - 1
	}
	if viewportHeight < 1 {
		return 1
	}
	return viewportHeight
}

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
