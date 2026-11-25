package app

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/travisennis/cake-repl/internal/cake"
	"github.com/travisennis/cake-repl/internal/childproc"
	"github.com/travisennis/cake-repl/internal/ui"
)

type Config struct {
	CakeBin              string
	Cwd                  string
	RestoreCwd           bool
	Explicit             map[string]bool
	ReloadProjectHelpers func(cwd string) (ProjectHelpers, error)
	Model                string
	Profile              string
	Tools                string
	NoTools              bool
	AddDirs              []string
	ToolboxDirs          []string
	Sandbox              string
	NoSkills             bool
	Skills               string
	SystemPrompt         string
	Fork                 bool
	ForkID               string
	HydrateID            string
	HydrateFork          bool
	NoSession            bool
	InitialMode          cake.RunMode
	ResumeID             string
	StartupPicker        bool
	DebugLog             io.Writer
	Runs                 *cake.Registry
	Children             *childproc.Registry
	HistoryFile          string
	OutputLimit          int
	MaxTimelineItems     int
	ToolColor            bool
	Inline               bool
	Compaction           CompactionConfig
	Suggestions          SuggestionConfig
	Watch                WatchConfig
}

type turnSelection struct {
	Model           string
	Profile         string
	ModelExplicit   bool
	ProfileExplicit bool
}

type ProjectHelpers struct {
	CompactionEnabled  bool
	SuggestionsEnabled bool
	WatchEnabled       bool
}

type CompactionConfig struct {
	Enabled          bool
	Bin              string
	MaxContextTokens int
	TriggerTokens    int
	ThresholdPercent int
	KeepTokens       int
	Model            string
	Profile          string
	ReasoningEffort  string
}

type SuggestionConfig struct {
	Enabled         bool
	Bin             string
	Model           string
	Profile         string
	ReasoningEffort string
}

type WatchConfig struct {
	Enabled  bool
	Interval time.Duration
	Sources  []WatchSource
}

type WatchSource struct {
	Name    string
	Path    string
	Pattern string
	Prompt  string
}

const (
	minInputHeight           = 3
	maxInputHeight           = 8
	composerVerticalChrome   = 2
	composerHorizontalChrome = 2
	statusHeight             = 1
	maxHistoryEntries        = 1000
	jsonlPrefix              = "\x02"
	maxHistoryRecordBytes    = 1 << 20
	maxComposerLines         = 1000
	maxComposerBytes         = 1 << 20

	defaultInputPlaceholder = "Type a prompt…"

	inlineViewportMax  = 12
	frameSafetyColumns = 2
)

type Model struct {
	cfg   Config
	theme ui.Theme
	keys  keyMap

	width  int
	height int
	ready  bool

	input     textarea.Model
	viewport  viewport.Model
	timeline  timeline
	noticeSeq uint64
	spin      spinner.Model

	runOp  pendingOperation
	run    *cake.Run
	runSeq uint64

	runPrompt string

	replay        replayState
	trustDeferred bool

	sawComplete   bool
	runOutcome    outcome
	runGeneration sessionGeneration

	exitPhase         phase
	compaction        pendingOperation
	compactionStart   time.Time
	compactionElapsed time.Duration
	compactionNotice  noticeID
	compactionSeq     uint64
	native            nativeCompactionState
	suggestion        suggestionState
	watch             watchState

	session        sessionState
	turn           turnSelection
	history        promptHistory
	pendingCalls   map[string]*ui.ToolBlock
	toolOutputMode ui.ToolOutputMode

	completionPrefix  string
	completionMatches []string
	completionIdx     int

	browser     sessionBrowser
	sessionList list.Model

	startupCmd tea.Cmd

	sessionListSeq    uint64
	sessionListReq    uint64
	sessionListCancel context.CancelFunc
	sessionListDone   map[uint64]chan struct{}
}

func New(cfg Config) Model {
	th := ui.DefaultTheme()
	th.ToolColor = cfg.ToolColor

	input := textarea.New()
	input.Placeholder = defaultInputPlaceholder
	input.FocusedStyle.Placeholder = th.Placeholder
	input.BlurredStyle.Placeholder = th.Placeholder
	input.ShowLineNumbers = false
	input.CharLimit = 0
	input.MaxHeight = maxComposerLines
	input.SetHeight(minInputHeight)
	input.Focus()

	spin := spinner.New()
	spin.Spinner = spinner.MiniDot

	m := Model{
		cfg:             cfg,
		theme:           th,
		keys:            defaultKeyMap(),
		input:           input,
		spin:            spin,
		sessionList:     newSessionList(),
		sessionListDone: map[uint64]chan struct{}{},
		pendingCalls:    map[string]*ui.ToolBlock{},
		toolOutputMode:  ui.ToolOutputHidden,
		session:         newSessionState(cfg.InitialMode, cfg.ResumeID),
		turn: turnSelection{
			Model:           cfg.Model,
			Profile:         cfg.Profile,
			ModelExplicit:   cfg.Explicit["model"] && cfg.Model != "",
			ProfileExplicit: cfg.Explicit["profile"] && cfg.Profile != "",
		},
		watch:         newWatchState(cfg.Watch),
		trustDeferred: cfg.RestoreCwd,
	}
	m.watch.prime(m.watchSessionID(), m.cfg.Cwd)
	if cfg.HydrateID != "" {
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		m.replay.begin(ctx, m.session.Generation(), cfg.HydrateID, cfg.HydrateFork, cancel, done)
		m.trackChild("replay", cancel, done)
	}
	m.appendItem(ui.Item{Kind: ui.KindInfo, Text: "cake-repl — /help for commands"})

	if cfg.StartupPicker {
		m, m.startupCmd = m.openSessionBrowser()
	}

	if cfg.HistoryFile != "" {
		m.loadHistory(cfg.HistoryFile)
	}

	return m
}

func (m *Model) loadHistory(path string) {
	f, err := os.Open(path) //nolint:gosec // user-provided path from -history-file
	if err != nil {
		return
	}
	defer f.Close() //nolint:errcheck // best-effort on close

	entries, skipped, err := readHistory(f)

	if err == nil && len(entries) > maxHistoryEntries {
		entries = entries[len(entries)-maxHistoryEntries:]
		var b strings.Builder
		for _, e := range entries {
			b.WriteString(encodeHistoryEntry(e))
		}
		_ = os.WriteFile(path, []byte(b.String()), 0o600)
	}

	m.history.entries = entries
	m.history.idx = len(entries)

	if skipped > 0 {
		m.appendItem(ui.Item{Kind: ui.KindWarning, Text: historySkipWarning(skipped)})
	}
}

func readHistory(r io.Reader) (entries []string, skipped int, err error) {
	br := bufio.NewReaderSize(r, maxHistoryRecordBytes)
	for {
		line, rerr := br.ReadSlice('\n')
		if errors.Is(rerr, bufio.ErrBufferFull) {
			skipped++
			for errors.Is(rerr, bufio.ErrBufferFull) {
				_, rerr = br.ReadSlice('\n')
			}
			if rerr != nil && !errors.Is(rerr, io.EOF) {
				return entries, skipped, rerr
			}
			if errors.Is(rerr, io.EOF) {
				return entries, skipped, nil
			}
			continue
		}
		if rerr != nil && !errors.Is(rerr, io.EOF) {
			return entries, skipped, rerr
		}
		if s := trimHistoryLine(line); s != "" {
			if strings.HasPrefix(s, jsonlPrefix) {
				var v string
				if json.Unmarshal([]byte(s[len(jsonlPrefix):]), &v) == nil {
					entries = append(entries, v)
				} else {
					entries = append(entries, s)
				}
			} else {
				entries = append(entries, s)
			}
		}
		if errors.Is(rerr, io.EOF) {
			return entries, skipped, nil
		}
	}
}

func trimHistoryLine(line []byte) string {
	s := strings.TrimSuffix(string(line), "\n")
	return strings.TrimSuffix(s, "\r")
}

func historySkipWarning(n int) string {
	return fmt.Sprintf("skipped %d oversized history record(s) (over %d MiB)", n, maxHistoryRecordBytes>>20)
}

func historyTooLargeWarning() string {
	return fmt.Sprintf("prompt too large to save to history (over %d MiB)", maxHistoryRecordBytes>>20)
}

func encodeHistoryEntry(text string) string {
	j, _ := json.Marshal(text)
	return jsonlPrefix + string(j) + "\n"
}

func (m Model) CancelRunning() error {
	if m.cfg.Children != nil {
		return m.cfg.Children.Shutdown(ChildTeardownTimeout)
	}
	return m.cancelRunning(ChildTeardownTimeout)
}

func (m *Model) cancelReplay() {
	m.replay.cancel()
}

func (m *Model) cancelCompaction() {
	m.compaction.cancel()
}

func (m *Model) cancelRun() {
	m.runOp.cancel()
}

func (m Model) SessionData() (sessionID, cwd string) {
	if m.session.SessionID != "" {
		return m.session.SessionID, m.cfg.Cwd
	}
	if m.session.ResumeID != "" {
		return m.session.ResumeID, m.cfg.Cwd
	}
	return "", m.cfg.Cwd
}

func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{
		textarea.Blink,
		m.titleCmd(),
	}
	if !m.trustDeferred {
		if cmd := startTrustProbeCmd(m.cfg); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	if m.startupCmd != nil {
		cmds = append(cmds, m.startupCmd)
	}
	if m.watchEnabled() {
		cmds = append(cmds, watchTickCmd(m.cfg.Watch.Interval))
	}
	if m.replay.phase() == replayLoading {
		cmds = append(cmds, startReplayCmd(m.cfg, m.replay.op.key, m.replay.ctx, m.replay.done))
	}
	return tea.Batch(cmds...)
}

func windowTitle(cwd, marker string) string {
	title := "cake-repl: " + ui.Sanitize(cwd)
	if marker != "" {
		title = marker + " " + title
	}
	return title
}

func (m Model) titleCmd() tea.Cmd {
	return tea.SetWindowTitle(windowTitle(m.cfg.Cwd, m.presentationState().titleMarker))
}

func IdleTitleSequence(cwd string) string {
	return ansi.SetWindowTitle(windowTitle(cwd, ""))
}

func (m *Model) renderTimelineItem(it ui.Item, width int) string {
	return ui.RenderItem(m.theme, it, width, m.cfg.OutputLimit, m.toolOutputMode)
}

func (m *Model) appendItem(it ui.Item) {
	if m.timeline.append(it, m.cfg.MaxTimelineItems, m.renderTimelineItem) > 0 {
		m.dropDetachedPendingCalls()
	}
}

func (m *Model) firstPendingToolIdx() int {
	i := len(m.timeline.items) - 1
	for i >= 0 && m.timeline.items[i].Kind == ui.KindTool && m.timeline.items[i].Tool != nil && !m.timeline.items[i].Tool.Done {
		i--
	}
	i++
	if i < len(m.timeline.items) {
		return i
	}
	return -1
}

func (m Model) lastAssistantMarkdown() (text string, ok bool) {
	for i := len(m.timeline.items) - 1; i >= 0; i-- {
		it := m.timeline.items[i]
		if it.Kind == ui.KindAssistant && strings.TrimSpace(it.Text) != "" {
			return it.Text, true
		}
	}
	return "", false
}

func (m *Model) lastItemIsReasoning() bool {
	return len(m.timeline.items) > 0 && m.timeline.items[len(m.timeline.items)-1].Kind == ui.KindReasoning
}

func (m *Model) removeItemAt(idx int) {
	if m.timeline.remove(idx) {
		m.dropDetachedPendingCalls()
	}
}

func (m *Model) insertItemAt(idx int, it ui.Item) {
	if m.timeline.insert(idx, it, m.cfg.MaxTimelineItems, m.renderTimelineItem) > 0 {
		m.dropDetachedPendingCalls()
	}
}

func (m *Model) toolIndex(tool *ui.ToolBlock) int {
	if tool == nil {
		return -1
	}
	for i := range m.timeline.items {
		if m.timeline.items[i].Tool == tool {
			return i
		}
	}
	return -1
}

func (m *Model) pendingTool(callID string) (tool *ui.ToolBlock, idx int, ok bool) {
	tool, ok = m.pendingCalls[callID]
	if !ok {
		return nil, -1, false
	}
	if idx = m.toolIndex(tool); idx >= 0 {
		return tool, idx, true
	}
	delete(m.pendingCalls, callID)
	return nil, -1, false
}

func (m *Model) dropDetachedPendingCalls() {
	if len(m.pendingCalls) == 0 {
		return
	}
	attached := make(map[*ui.ToolBlock]struct{}, len(m.timeline.items))
	for i := range m.timeline.items {
		if tool := m.timeline.items[i].Tool; tool != nil {
			attached[tool] = struct{}{}
		}
	}
	for callID, tool := range m.pendingCalls {
		if _, ok := attached[tool]; !ok {
			delete(m.pendingCalls, callID)
		}
	}
}

func (m *Model) rerenderItem(idx int) {
	m.timeline.rerender(idx, m.renderTimelineItem)
}

func (m *Model) rerenderToolItems() {
	m.timeline.rerenderTools(m.renderTimelineItem)
}

func (m *Model) syncViewport() {
	if !m.timeline.takeDirty() {
		return
	}
	atBottom := m.viewport.AtBottom()
	yOffset := m.viewport.YOffset
	m.viewport.SetContent(m.timeline.content())
	if atBottom {
		m.viewport.GotoBottom()
	} else {
		m.viewport.SetYOffset(yOffset)
	}
}

func (m *Model) layout() {
	if m.width <= 0 || m.height <= 0 {
		return
	}
	frameWidth := m.frameWidth()
	inputHeight := inputHeightFor(m.input.LineCount())
	inputWidth := frameWidth - composerHorizontalChrome
	if inputWidth < 1 {
		inputWidth = 1
	}
	m.input.SetWidth(inputWidth)
	m.input.SetHeight(inputHeight)

	vpHeight := m.height - inputHeight - composerVerticalChrome - statusHeight
	if m.cfg.Inline {
		vpHeight = inlineViewportHeight(m.height, inputHeight)
	}
	if vpHeight < 1 {
		vpHeight = 1
	}
	m.resizeSessionList(vpHeight)
	if !m.ready {
		m.viewport = viewport.New(frameWidth, vpHeight)
		m.ready = true
		m.timeline.resize(frameWidth, m.renderTimelineItem)
		return
	}

	atBottom := m.viewport.AtBottom()
	m.viewport.Height = vpHeight
	if m.viewport.Width != frameWidth {
		m.viewport.Width = frameWidth
		m.timeline.resize(frameWidth, m.renderTimelineItem)
		return
	}
	if atBottom {
		m.viewport.GotoBottom()
	}
}

func (m Model) frameWidth() int {
	w := m.width - frameSafetyColumns
	if w < 1 {
		return 1
	}
	return w
}

func inputHeightFor(n int) int {
	if n < minInputHeight {
		return minInputHeight
	}
	if n > maxInputHeight {
		return maxInputHeight
	}
	return n
}

func inlineViewportHeight(height, inputHeight int) int {
	vp := height - inputHeight - composerVerticalChrome - statusHeight
	if vp > inlineViewportMax {
		vp = inlineViewportMax
	}
	if half := height/2 - inputHeight - composerVerticalChrome - statusHeight; vp > half {
		vp = half
	}
	if vp < 1 {
		vp = 1
	}
	return vp
}

func (m Model) ComposerRows() int {
	if !m.ready {
		return 0
	}
	return inputHeightFor(m.input.LineCount()) + composerVerticalChrome
}
