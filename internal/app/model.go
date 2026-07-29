// Package app implements the cake-repl Bubble Tea program: a full-screen
// terminal REPL that drives cake subprocesses and renders their stream-json
// output live.
package app

import (
	"bufio"
	"encoding/json"
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

// Config is everything main passes in from CLI flags and config files.
type Config struct {
	CakeBin          string
	Cwd              string
	Model            string
	Profile          string
	InitialMode      cake.RunMode
	ResumeID         string
	DebugLog         io.Writer
	HistoryFile      string
	OutputLimit      int
	MaxTimelineItems int
}

const (
	minInputHeight           = 3
	maxInputHeight           = 8
	composerVerticalChrome   = 2
	composerHorizontalChrome = 2
	statusHeight             = 1
	maxHistoryEntries        = 1000
	jsonlPrefix              = "\x02" // per-record marker for JSONL entries
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
	// newSessionPending is true while an old run canceled by Ctrl+N is being
	// drained. Its remaining events and cancellation result belong before the
	// new-session boundary and must not affect the fresh state.
	newSessionPending bool

	session      sessionState
	history      promptHistory
	pendingCalls map[string]int // call_id -> index into items
	items        []ui.Item
	// toolOutputMode applies to every tool block in the timeline and to tool
	// blocks appended later in this REPL session.
	toolOutputMode ui.ToolOutputMode

	// rendered caches one rendered string per item at renderedWidth.
	// timelineContent caches the viewport payload so appends do not join the
	// full rendered slice on the hot path.
	rendered        []string
	renderedWidth   int
	timelineContent string

	// completion state for Tab cycling; zero values mean no active cycle.
	completionPrefix  string
	completionMatches []string
	completionIdx     int
}

// New builds the initial model.
func New(cfg Config) Model {
	th := ui.DefaultTheme()

	input := textarea.New()
	input.Placeholder = "Type a prompt…"
	input.FocusedStyle.Placeholder = th.Placeholder
	input.BlurredStyle.Placeholder = th.Placeholder
	input.ShowLineNumbers = false
	input.CharLimit = 0
	input.SetHeight(minInputHeight)
	input.Focus()

	spin := spinner.New()
	spin.Spinner = spinner.MiniDot

	m := Model{
		cfg:          cfg,
		theme:        th,
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

// loadHistory reads a history file into the prompt history state machine.
//
// Each line starting with jsonlPrefix ("\x02") is decoded as a JSON-encoded
// string; all other lines are treated as plain-text legacy entries. Blank
// lines are skipped.
//
// Entries beyond maxHistoryEntries are trimmed on load and the surviving
// entries are rewritten with the JSONL prefix, migrating legacy files forward.
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
		line := sc.Text()
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, jsonlPrefix) {
			var s string
			if err := json.Unmarshal([]byte(line[len(jsonlPrefix):]), &s); err == nil {
				entries = append(entries, s)
				continue
			}
		}
		// Plain text (legacy entry or unparseable JSONL) — keep verbatim.
		entries = append(entries, line)
	}
	if err := sc.Err(); err != nil {
		return // best-effort
	}

	if len(entries) > maxHistoryEntries {
		entries = entries[len(entries)-maxHistoryEntries:]
		// Rewrite the file to stay under the cap. Errors are best-effort.
		// Write every retained entry with the JSONL prefix, migrating any
		// plain-text legacy entries forward.
		var b strings.Builder
		for _, e := range entries {
			b.WriteString(encodeHistoryEntry(e))
		}
		_ = os.WriteFile(path, []byte(b.String()), 0o600)
	}

	m.history.entries = entries
	m.history.idx = len(entries)
}

func encodeHistoryEntry(text string) string {
	j, _ := json.Marshal(text)
	return jsonlPrefix + string(j) + "\n"
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

// trimFront removes the first `over` items from the timeline, adjusts
// pendingCalls indices, and drops entries for trimmed-away pending calls.
func (m *Model) trimFront(over int) {
	m.items = m.items[over:]
	for callID, callIdx := range m.pendingCalls {
		if callIdx < over {
			delete(m.pendingCalls, callID)
		} else {
			m.pendingCalls[callID] = callIdx - over
		}
	}
}

// appendItem adds a timeline item, renders it into the cache, and returns
// its index.
func (m *Model) appendItem(it ui.Item) int {
	m.items = append(m.items, it)
	if m.cfg.MaxTimelineItems > 0 && len(m.items) > m.cfg.MaxTimelineItems {
		over := len(m.items) - m.cfg.MaxTimelineItems
		m.trimFront(over)
		if m.ready {
			m.rendered = m.rendered[over:]
			m.rebuildTimelineContent()
		}
	}
	if m.ready {
		rendered := ui.RenderItem(m.theme, it, m.renderedWidth, m.cfg.OutputLimit, m.toolOutputMode)
		m.rendered = append(m.rendered, rendered)
		m.appendTimelineContent(rendered)
		m.syncViewport()
	}
	return len(m.items) - 1
}

// firstPendingToolIdx returns the index of the first pending (not yet done)
// KindTool item when all trailing items are pending tools, or -1 otherwise.
// This is used to insert assistant messages before pending tool calls when
// cake emits function_call events before message events.
func (m *Model) firstPendingToolIdx() int {
	i := len(m.items) - 1
	for i >= 0 && m.items[i].Kind == ui.KindTool && m.items[i].Tool != nil && !m.items[i].Tool.Done {
		i--
	}
	i++ // first pending tool, or len(items) if none found
	if i < len(m.items) {
		return i
	}
	return -1
}

// insertItemAt inserts an item at the given index, shifting subsequent items
// to the right, and updates the render cache and pending call indices.
func (m *Model) insertItemAt(idx int, it ui.Item) {
	m.items = append(m.items, ui.Item{}) // make room
	copy(m.items[idx+1:], m.items[idx:])
	m.items[idx] = it

	// Adjust pendingCalls indices for the insertion shift.
	for callID, callIdx := range m.pendingCalls {
		if callIdx >= idx {
			m.pendingCalls[callID] = callIdx + 1
		}
	}

	if m.cfg.MaxTimelineItems > 0 && len(m.items) > m.cfg.MaxTimelineItems {
		over := len(m.items) - m.cfg.MaxTimelineItems
		m.trimFront(over)
		if m.ready {
			m.rebuildTimeline()
		}
		return
	}

	if m.ready {
		rendered := ui.RenderItem(m.theme, it, m.renderedWidth, m.cfg.OutputLimit, m.toolOutputMode)
		m.rendered = append(m.rendered, "") // make room
		copy(m.rendered[idx+1:], m.rendered[idx:])
		m.rendered[idx] = rendered
		m.rebuildTimelineContent()
		m.syncViewport()
	}
}

// rerenderItem refreshes one item's cached rendering after it mutated in
// place (a tool block receiving its output).
func (m *Model) rerenderItem(idx int) {
	if !m.ready || idx < 0 || idx >= len(m.rendered) {
		return
	}
	m.rendered[idx] = ui.RenderItem(m.theme, m.items[idx], m.renderedWidth, m.cfg.OutputLimit, m.toolOutputMode)
	m.rebuildTimelineContent()
	m.syncViewport()
}

// rerenderToolItems refreshes every tool item's cached rendering while
// reusing the cached strings for all other timeline item kinds.
func (m *Model) rerenderToolItems() {
	if !m.ready {
		return
	}
	for i, it := range m.items {
		if it.Kind == ui.KindTool {
			m.rendered[i] = ui.RenderItem(m.theme, it, m.renderedWidth, m.cfg.OutputLimit, m.toolOutputMode)
		}
	}
	m.rebuildTimelineContent()
	m.syncViewport()
}

// rebuildTimeline re-renders every item at the current viewport width. Width
// changes and /clear use this; other changes update the cache incrementally.
func (m *Model) rebuildTimeline() {
	if !m.ready {
		return
	}
	m.renderedWidth = m.timeline.Width
	m.rendered = m.rendered[:0]
	for _, it := range m.items {
		m.rendered = append(m.rendered, ui.RenderItem(m.theme, it, m.renderedWidth, m.cfg.OutputLimit, m.toolOutputMode))
	}
	m.rebuildTimelineContent()
	m.syncViewport()
}

func (m *Model) appendTimelineContent(rendered string) {
	if m.timelineContent != "" {
		m.timelineContent += "\n\n"
	}
	m.timelineContent += rendered
}

func (m *Model) rebuildTimelineContent() {
	m.timelineContent = strings.Join(m.rendered, "\n\n")
}

// syncViewport pushes the cached timeline content into the viewport, keeping
// the view pinned to the bottom if it was there already. Otherwise it restores
// the previous scroll offset so a single item re-render does not move the view.
func (m *Model) syncViewport() {
	atBottom := m.timeline.AtBottom()
	yOffset := m.timeline.YOffset
	m.timeline.SetContent(m.timelineContent)
	if atBottom {
		m.timeline.GotoBottom()
	} else {
		m.timeline.SetYOffset(yOffset)
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
	inputWidth := m.width - composerHorizontalChrome
	if inputWidth < 1 {
		inputWidth = 1
	}
	m.input.SetWidth(inputWidth)
	m.input.SetHeight(inputHeight)

	vpHeight := m.height - inputHeight - composerVerticalChrome - statusHeight
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
