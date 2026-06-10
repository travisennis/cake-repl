// Package app implements the cake-repl Bubble Tea program: a full-screen
// terminal REPL that drives cake subprocesses and renders their stream-json
// output live.
package app

import (
	"io"

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
}

const (
	minInputHeight = 3
	maxInputHeight = 8
	statusHeight   = 1
	ruleHeight     = 1
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
	pendingCalls map[string]int // call_id -> index into items
	items        []ui.Item
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
	return m
}

// Init implements tea.Model.
func (m Model) Init() tea.Cmd {
	return textarea.Blink
}

// appendItem adds a timeline item and returns its index.
func (m *Model) appendItem(it ui.Item) int {
	m.items = append(m.items, it)
	m.refreshTimeline()
	return len(m.items) - 1
}

// refreshTimeline re-renders all items into the viewport, keeping the view
// pinned to the bottom if it was there already.
func (m *Model) refreshTimeline() {
	if !m.ready {
		return
	}
	atBottom := m.timeline.AtBottom()
	m.timeline.SetContent(ui.RenderItems(m.theme, m.items, m.timeline.Width))
	if atBottom {
		m.timeline.GotoBottom()
	}
}

// layout recomputes component sizes from the terminal size and input
// contents.
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
	} else {
		m.timeline.Width = m.width
		m.timeline.Height = vpHeight
	}
	m.refreshTimeline()
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
