// Package app implements the cake-repl Bubble Tea program: a full-screen
// terminal REPL that drives cake subprocesses and renders their stream-json
// output live.
package app

import (
	"bufio"
	"io"
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/travisennis/cake-repl/internal/cake"
	"github.com/travisennis/cake-repl/internal/ui"
)

// Config is everything main passes in from CLI flags.
type Config struct {
	CakeBin     string
	Cwd         string
	Model       string
	Profile     string
	InitialMode cake.RunMode
	ResumeID    string
	DebugLog    io.Writer
	HistoryFile string
}

const (
	minInputHeight    = 3
	maxInputHeight    = 8
	statusHeight      = 1
	ruleHeight        = 1
	maxHistoryEntries = 1000
)

// Model is the Bubble Tea model for the whole REPL.
type Model struct {
	cfg   Config
	theme ui.Theme
	keys  keyMap

	width  int
	height int
	ready  bool

	input    textarea.Model
	timeline viewport.Model
	spin     spinner.Model

	running     bool
	run         *cake.Run
	sawComplete bool
	exitAfter   bool

	session      sessionState
	history      promptHistory
	pendingCalls map[string]int // call_id -> index into items
	items        []ui.Item

	// rendered caches one rendered string per item at renderedWidth, so
	// appends and single-item updates do not re-render the whole timeline.
	rendered      []string
	renderedWidth int

	// completion state for Tab cycling; zero values mean no active cycle.
	completionPrefix  string
	completionMatches []string
	completionIdx     int
}

// New builds the initial model.
func New(cfg Config) Model {
	input := textarea.New()
	input.Placeholder = "Type a prompt. Enter = newline, Ctrl+S = submit, /help for commands."
	input.ShowLineNumbers = false
	input.CharLimit = 0
	input.SetHeight(minInputHeight)
	input.Focus()

	spin := spinner.New()
	spin.Spinner = spinner.MiniDot

	m := Model{
		cfg:          cfg,
		theme:        ui.DefaultTheme(),
		keys:         defaultKeyMap(),
		input:        input,
		spin:         spin,
		pendingCalls: map[string]int{},
		session: sessionState{
			NextMode: cfg.InitialMode,
			ResumeID: cfg.ResumeID,
		},
	}
	m.appendItem(ui.Item{Kind: ui.KindInfo, Text: "cake-repl — /help for commands"})

	if cfg.HistoryFile != "" {
		m.loadHistory(cfg.HistoryFile)
	}

	return m
}

// loadHistory reads a newline-terminated history file into the prompt history
// state machine. Entries beyond maxHistoryEntries are trimmed on load.
func (m *Model) loadHistory(path string) {
	f, err := os.Open(path) //nolint:gosec // user-provided path from -history-file
	if err != nil {
		return
	}
	defer f.Close() //nolint:errcheck // best-effort on close

	var entries []string
	sc := bufio.NewScanner(f)
	buf := make([]byte, 64*1024)
	sc.Buffer(buf, 1024*1024)
	for sc.Scan() {
		if line := sc.Text(); line != "" {
			entries = append(entries, line)
		}
	}
	if err := sc.Err(); err != nil {
		return // best-effort
	}

	if len(entries) > maxHistoryEntries {
		entries = entries[len(entries)-maxHistoryEntries:]
		// Rewrite the file to stay under the cap. Errors are best-effort.
		data := strings.Join(entries, "\n") + "\n"
		_ = os.WriteFile(path, []byte(data), 0o600)
	}

	m.history.entries = entries
	m.history.idx = len(entries)
}

// CancelRunning cancels any active cake subprocess. Safe to call when
// nothing is running.
func (m Model) CancelRunning() {
	if m.run != nil {
		m.run.Cancel()
	}
}

// SessionData returns the current session ID and working directory for
// display after the TUI exits. If no task has run yet it falls back to the
// resume ID when one was provided via -resume.
func (m Model) SessionData() (sessionID, cwd string) {
	if m.session.SessionID != "" {
		return m.session.SessionID, m.cfg.Cwd
	}
	if m.session.ResumeID != "" {
		return m.session.ResumeID, m.cfg.Cwd
	}
	return "", m.cfg.Cwd
}

// Init implements tea.Model.
func (m Model) Init() tea.Cmd {
	return textarea.Blink
}

// appendItem adds a timeline item, renders it into the cache, and returns
// its index.
func (m *Model) appendItem(it ui.Item) int {
	m.items = append(m.items, it)
	if m.ready {
		m.rendered = append(m.rendered, ui.RenderItem(m.theme, it, m.renderedWidth))
		m.syncViewport()
	}
	return len(m.items) - 1
}

// rerenderItem refreshes one item's cached rendering after it mutated in
// place (a tool block receiving its output).
func (m *Model) rerenderItem(idx int) {
	if !m.ready || idx < 0 || idx >= len(m.rendered) {
		return
	}
	m.rendered[idx] = ui.RenderItem(m.theme, m.items[idx], m.renderedWidth)
	m.syncViewport()
}

// rebuildTimeline re-renders every item at the current viewport width. Only
// width changes (and /clear) need this; everything else updates the cache
// incrementally.
func (m *Model) rebuildTimeline() {
	if !m.ready {
		return
	}
	m.renderedWidth = m.timeline.Width
	m.rendered = m.rendered[:0]
	for _, it := range m.items {
		m.rendered = append(m.rendered, ui.RenderItem(m.theme, it, m.renderedWidth))
	}
	m.syncViewport()
}

// syncViewport pushes the cached renderings into the viewport, keeping the
// view pinned to the bottom if it was there already.
func (m *Model) syncViewport() {
	atBottom := m.timeline.AtBottom()
	m.timeline.SetContent(strings.Join(m.rendered, "\n\n"))
	if atBottom {
		m.timeline.GotoBottom()
	}
}

// layout recomputes component sizes from the terminal size and input
// contents. The timeline is re-rendered only when its width changes; height
// changes just resize the viewport window.
func (m *Model) layout() {
	if m.width <= 0 || m.height <= 0 {
		return
	}
	inputHeight := clamp(m.input.LineCount(), minInputHeight, maxInputHeight)
	m.input.SetWidth(m.width)
	m.input.SetHeight(inputHeight)

	vpHeight := m.height - inputHeight - statusHeight - ruleHeight
	if vpHeight < 1 {
		vpHeight = 1
	}
	if !m.ready {
		m.timeline = viewport.New(m.width, vpHeight)
		m.ready = true
		m.rebuildTimeline()
		return
	}

	atBottom := m.timeline.AtBottom()
	m.timeline.Height = vpHeight
	if m.timeline.Width != m.width {
		m.timeline.Width = m.width
		m.rebuildTimeline()
		return
	}
	if atBottom {
		m.timeline.GotoBottom()
	}
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
