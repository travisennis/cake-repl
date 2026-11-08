// Package app implements the cake-repl Bubble Tea program: a full-screen
// terminal REPL that drives cake subprocesses and renders their stream-json
// output live.
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

	"github.com/atotto/clipboard"
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

// Config is everything main passes in from CLI flags and config files.
type Config struct {
	CakeBin      string
	Cwd          string
	Model        string
	Profile      string
	Tools        string
	NoTools      bool
	AddDirs      []string
	ToolboxDirs  []string
	Sandbox      string
	NoSkills     bool
	Skills       string
	SystemPrompt string
	Fork         bool
	ForkID       string
	NoSession    bool
	InitialMode  cake.RunMode
	ResumeID     string
	// StartupPicker opens the /sessions browser as soon as the program starts,
	// for a bare `-resume`. It asks the user to choose a session instead of
	// naming one: nothing is hydrated and no session is pinned until a row is
	// picked. See ADR 021.
	StartupPicker bool
	DebugLog      io.Writer
	// Runs tracks live and replay engine subprocesses so the suspend cycle can
	// stop and continue their process groups. Nil leaves them untracked.
	Runs *cake.Registry
	// Children is the process-level registry main owns for unobserved-exit
	// teardown. Every child operation registers here when it starts, so main can
	// cancel and reap it even when Bubble Tea returns no model after an Update or
	// View panic. Nil falls back to Model.CancelRunning's model-owned handles.
	Children         *childproc.Registry
	HistoryFile      string
	OutputLimit      int
	MaxTimelineItems int
	// ToolColor lets tool output keep its own ANSI color instead of rendering
	// plain. Off by default; see ADR 009.
	ToolColor bool
	// Inline renders the live region below the terminal height instead of in
	// the alternate screen, so recent terminal history stays visible above the
	// REPL. See ADR 015.
	Inline bool
	// Compaction is the opt-in between-turn context compaction configuration.
	// The zero value is disabled, so a caller that does not wire it behaves
	// exactly as before.
	Compaction CompactionConfig
	// Suggestions is the opt-in after-turn prompt-suggestion configuration.
	// The zero value is disabled.
	Suggestions SuggestionConfig
}

// turnSelection is the model and profile the next cake invocation uses. It is
// initialized from Config's startup flags and config file values, may be
// changed by /model and /profile, and keeps an empty value as cake's own
// default. It lives on Model rather than sessionState because /new, Ctrl+N, and
// /resume preserve it. See ADR 007.
type turnSelection struct {
	Model   string
	Profile string
}

// CompactionConfig is the resolved `[compaction]` configuration. Bin is the
// external compactor executable, MaxContextTokens is the quality ceiling for
// absolute mode, TriggerTokens is the absolute compaction line, and
// ThresholdPercent/KeepTokens are the percent fallback and retained-tail target.
// Model, Profile, and ReasoningEffort select the summary call; empty values
// fall back to the current turn selection and cake's own reasoning default. The
// trigger and ceiling are between-turn targets, not in-invocation caps; the
// ceiling is policy only and is never passed to the compactor. See ADR 023,
// ADR 025, and ADR 029.
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

// SuggestionConfig is the resolved `[suggestions]` configuration. Bin is the
// external suggester executable. Model and Profile select the suggestion call;
// empty values fall back to the current turn selection. ReasoningEffort is
// passed through when set and otherwise leaves Cake's default. See ADR 028,
// ADR 030, and ADR 032.
type SuggestionConfig struct {
	Enabled         bool
	Bin             string
	Model           string
	Profile         string
	ReasoningEffort string
}

const (
	minInputHeight           = 3
	maxInputHeight           = 8
	composerVerticalChrome   = 2
	composerHorizontalChrome = 2
	statusHeight             = 1
	maxHistoryEntries        = 1000
	jsonlPrefix              = "\x02" // per-record marker for JSONL entries
	// maxHistoryRecordBytes bounds one persisted history record, measured as
	// its full JSONL line (the \x02 prefix, the JSON-encoded prompt, and the
	// newline). The reader's buffer is fixed at this size, so it always fits
	// one record: persistHistory writes only records within the bound, and
	// loadHistory drains and skips larger ones while keeping the records around
	// them, instead of failing the whole load.
	maxHistoryRecordBytes = 1 << 20 // 1 MiB

	// defaultInputPlaceholder is the composer placeholder when no suggestion is
	// showing. A suggestion replaces it with the same dim styling, so accepting
	// or dismissing a suggestion always returns to this one value.
	defaultInputPlaceholder = "Type a prompt…"

	// inlineViewportMax caps the timeline viewport in inline mode so the live
	// region never grows tall enough to push the terminal's own history out of
	// the visible window (ADR 015).
	inlineViewportMax = 12
)

// Model is the Bubble Tea model for the whole REPL.
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

	// runOp owns the live run's phase, request identity, cancellation, and
	// reaping channel. run is the subprocess handle while the run is active or
	// draining. runSeq allocates the per-run request identity. A run remains a
	// resource gate until its result is settled, even while it drains.
	runOp  pendingOperation
	run    *cake.Run
	runSeq uint64

	// replay owns the startup replay request, its subprocess, and the
	// post-boundary drain. Its phase keeps the resource gate closed until the
	// canceled child is reaped, while the request key suppresses records after
	// the user leaves the session.
	replay replayState

	// sawComplete and runOutcome describe the last run's result, not a lifecycle
	// phase. runGeneration scopes that result so a later session boundary can
	// invalidate it. completedTurn is the single answer post-turn operations use.
	sawComplete   bool
	runOutcome    outcome
	runGeneration sessionGeneration

	// exitPhase is idle or draining. Every quit path enters draining, cancels
	// every child, and returns tea.Quit only after all of them are reaped.
	exitPhase phase
	// compaction is the pending between-turn compactor. It owns the request
	// identity, cancellation, and reaping channel. A caller decides how its
	// active and draining phases gate input; compaction blocks prompt submission
	// and session-target changes so the next cake process cannot race its session
	// lock, and it marks the status line/title as busy.
	compaction pendingOperation
	// compactionStart is when the current compactor was launched and
	// compactionElapsed is its whole-second age, refreshed from the spinner tick
	// the REPL already runs while busy. Both are zero when no compaction is
	// pending, so the status line and composer omit the age. See ADR 026.
	compactionStart   time.Time
	compactionElapsed time.Duration
	// compactionNotice identifies the live start notice by its stable timeline
	// identity, not by row position or text. It is zero when no notice is
	// attached or the notice was resolved.
	compactionNotice noticeID
	// compactionSeq allocates a request identity that never repeats. The
	// pending operation owns the identity whose result may still be applied, so a
	// canceled or superseded run cannot touch the status line or context of a
	// later one.
	compactionSeq uint64
	// suggestion is the one read-only after-turn suggester request and its
	// presentation (ADR 028). The state owns the request identity, cancellation,
	// reaping channel, ghost text, and any result held for compaction. Its phase
	// is derived from those fields, so a request and its presentation cannot
	// disagree. A draft, a new turn, or a session change invalidates the request
	// so a late result cannot overwrite the composer. See ADR 031.
	suggestion suggestionState

	session      sessionState
	turn         turnSelection
	history      promptHistory
	pendingCalls map[string]*ui.ToolBlock // call_id -> pending tool block
	// toolOutputMode applies to every tool block in the timeline and to tool
	// blocks appended later in this REPL session. It starts hidden so a session
	// opens with one header per tool call; Ctrl+O reveals truncated and then
	// full output. See ADR 033.
	toolOutputMode ui.ToolOutputMode

	// contentChanged is set by the updates that reshape the live region's
	// visible content (a viewport push, a composer key) and read by the settle
	// sweep, which repaints the whole frame after it; see ADR 020.
	contentChanged bool

	// completion state for Tab cycling; zero values mean no active cycle.
	completionPrefix  string
	completionMatches []string
	completionIdx     int

	// browser is the /sessions overlay's one presentation phase. The listing
	// request identity, cancel function, and reaping channels below remain
	// separate because more than one canceled listing can still drain.
	browser     sessionBrowser
	sessionList list.Model

	// startupCmd is a command New built rather than Init. Init cannot mutate the
	// model, and the startup session listing needs the request identity and
	// cancel function startSessionListLoad records on the model, so the picker's
	// construction returns the pair to New and the command is issued here. Nil
	// when startup has no command of its own.
	startupCmd tea.Cmd

	// Session-listing operation state (ADR 019). The browser owns at most one
	// in-flight listing: sessionListSeq allocates a request identity that never
	// repeats, sessionListReq is the identity whose result may still be applied,
	// and sessionListCancel stops the request's helper. Closing the browser,
	// starting a replacement listing, and quitting all cancel and invalidate the
	// active request, so a superseded result cannot touch the browser.
	sessionListSeq    uint64
	sessionListReq    uint64
	sessionListCancel context.CancelFunc
	// sessionListDone tracks every listing child by request identity. A
	// replacement cancels the prior listing but does not wait for it, so more
	// than one canceled helper can still be draining; shutdown waits for all
	// of them.
	sessionListDone map[uint64]chan struct{}

	// clipboard writes text to the system clipboard. It is a field so tests
	// can stub out the platform helper; New defaults to the OS clipboard.
	clipboard func(string) error
}

// New builds the initial model.
func New(cfg Config) Model {
	th := ui.DefaultTheme()
	th.ToolColor = cfg.ToolColor

	input := textarea.New()
	input.Placeholder = defaultInputPlaceholder
	input.FocusedStyle.Placeholder = th.Placeholder
	input.BlurredStyle.Placeholder = th.Placeholder
	input.ShowLineNumbers = false
	input.CharLimit = 0
	// No line cap: pasting a long multi-line prompt must not freeze the Enter
	// (newline) key once the bubble textarea's default MaxHeight (99 lines) is
	// reached. Pasting bypasses MaxHeight, so a >99-line paste left Enter as a
	// no-op. Unbounded lets the prompt grow and scroll like the rest of the UI.
	input.MaxHeight = 0
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
		clipboard:       clipboard.WriteAll,
		toolOutputMode:  ui.ToolOutputHidden,
		session:         newSessionState(cfg.InitialMode, cfg.ResumeID),
		turn:            turnSelection{Model: cfg.Model, Profile: cfg.Profile},
	}
	if cfg.ResumeID != "" {
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		m.replay.begin(ctx, m.session.Generation(), cfg.ResumeID, cancel, done)
		m.trackChild("replay", cancel, done)
	}
	m.appendItem(ui.Item{Kind: ui.KindInfo, Text: "cake-repl — /help for commands"})

	if cfg.StartupPicker {
		// A bare `-resume` asks for a session instead of naming one, so the picker
		// opens in place of hydration; the picked row pins the next prompt through
		// the same transition as /resume. See ADR 021.
		m, m.startupCmd = m.openSessionBrowser()
	}

	if cfg.HistoryFile != "" {
		m.loadHistory(cfg.HistoryFile)
	}

	return m
}

// loadHistory reads a history file into the prompt history state machine.
//
// Each line starting with jsonlPrefix ("\x02") is decoded as a JSON-encoded
// string; all other lines are treated as plain-text legacy entries. Blank
// lines are skipped. A record whose line exceeds maxHistoryRecordBytes is
// drained and skipped, with a content-free warning, instead of aborting the
// load, so valid entries before and after it stay usable.
//
// Entries beyond maxHistoryEntries are trimmed on load and the surviving
// entries are rewritten with the JSONL prefix, migrating legacy files forward.
// The file is rewritten only after a complete read, and the surviving entries
// are still loaded; an oversized or unreadable file is never silently wiped.
func (m *Model) loadHistory(path string) {
	f, err := os.Open(path) //nolint:gosec // user-provided path from -history-file
	if err != nil {
		return
	}
	defer f.Close() //nolint:errcheck // best-effort on close

	entries, skipped, err := readHistory(f)

	if err == nil && len(entries) > maxHistoryEntries {
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

	if skipped > 0 {
		m.appendItem(ui.Item{Kind: ui.KindWarning, Text: historySkipWarning(skipped)})
	}
}

// readHistory parses history records from r. Each line starting with
// jsonlPrefix is decoded as a JSON-encoded string; every other line is a
// plain-text legacy entry, kept verbatim (including an unparseable JSONL
// line). Blank lines are skipped.
//
// A line longer than maxHistoryRecordBytes is drained to its newline in
// bounded chunks, counted in skipped, and the read resumes at the next record,
// so one oversized record cannot hide the records after it. The reader buffer
// is fixed at that bound, so memory stays flat no matter how large a record
// is. A complete read returns a nil error; a mid-file read error returns the
// entries read so far plus the error, letting the caller keep them without
// rewriting the file.
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
					// Unparseable JSONL — keep the line verbatim.
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

// trimHistoryLine turns one ReadSlice result into its entry text, dropping the
// trailing newline and an optional carriage return, mirroring bufio.ScanLines.
func trimHistoryLine(line []byte) string {
	s := strings.TrimSuffix(string(line), "\n")
	return strings.TrimSuffix(s, "\r")
}

// historySkipWarning is the content-free notice for records skipped at load:
// it names the count and the limit, never the dropped text.
func historySkipWarning(n int) string {
	return fmt.Sprintf("skipped %d oversized history record(s) (over %d MiB)", n, maxHistoryRecordBytes>>20)
}

// historyTooLargeWarning is the content-free notice for a submission too large
// to persist: it names the limit, never the prompt text.
func historyTooLargeWarning() string {
	return fmt.Sprintf("prompt too large to save to history (over %d MiB)", maxHistoryRecordBytes>>20)
}

func encodeHistoryEntry(text string) string {
	j, _ := json.Marshal(text)
	return jsonlPrefix + string(j) + "\n"
}

// CancelRunning cancels every active child and waits for it to be reaped, up to
// ChildTeardownTimeout. When main wired the process-level registry, it owns the
// child handles and this delegates to it; otherwise the model-owned handles are
// used. It is safe to call when nothing is running. The returned error names
// operations that did not finish within the bound; main reports it because the
// Update loop that would normally surface a failure is already gone.
func (m Model) CancelRunning() error {
	if m.cfg.Children != nil {
		return m.cfg.Children.Shutdown(ChildTeardownTimeout)
	}
	return m.cancelRunning(ChildTeardownTimeout)
}

// cancelReplay stops the startup replay whether it is still starting or already
// running. Canceling before cake.Replay starts prevents a child from being
// created after shutdown begins. The phase keeps the key until the reaped
// result settles the drain.
func (m *Model) cancelReplay() {
	m.replay.cancel()
}

// cancelCompaction stops an in-flight compactor. The pending operation keeps
// the same result identity until its compactionDoneMsg arrives, so the gate
// stays closed until the child has actually exited and been reaped.
func (m *Model) cancelCompaction() {
	m.compaction.cancel()
}

// cancelRun stops an in-flight run and keeps its request key so the reaped
// result still settles the phase. While idle this is a no-op.
func (m *Model) cancelRun() {
	m.runOp.cancel()
}

// SessionData returns the id of the session the REPL is continuing and the
// working directory, for display (the exit banner and /session copy). The
// reported id wins when an invocation has announced one; otherwise the pending
// -resume target is reported, so a retarget is exported before the new session
// has announced anything. Retargeting clears the reported id (see UseResume),
// so a session the REPL has left is never exported in its place.
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
	cmds := []tea.Cmd{
		textarea.Blink,
		// The settle sweep starts here and each sweep arms the next, so the
		// frame can heal from a terminal-side scroll for the program's life.
		// See ADR 020.
		settleSweepAtStartupCmd(),
		m.titleCmd(),
	}
	if cmd := startTrustProbeCmd(m.cfg); cmd != nil {
		cmds = append(cmds, cmd)
	}
	if m.startupCmd != nil {
		cmds = append(cmds, m.startupCmd)
	}
	if m.replay.phase() == replayLoading {
		cmds = append(cmds, startReplayCmd(m.cfg, m.replay.op.key, m.replay.ctx, m.replay.done))
	}
	return tea.Batch(cmds...)
}

// windowTitle renders the terminal title for a working directory and the
// presentation's title marker. It is the single place the title format is
// defined, so the idle form, every operation marker, and the sequence main
// writes on an unobserved exit cannot drift apart.
func windowTitle(cwd, marker string) string {
	title := "cake-repl: " + ui.Sanitize(cwd)
	if marker != "" {
		title = marker + " " + title
	}
	return title
}

// titleCmd returns a command that re-sets the terminal title from the current
// presentation. The status line and composer already name the operation, but
// neither is visible when the terminal window is unfocused, behind another
// window, or reduced to a tab, so the title carries the same signal: a marker
// prefixes the documented `cake-repl: <cwd>` title while an operation is active
// and is absent when the REPL is idle. Emit this on every presentation
// transition so the title never describes a state the REPL has left.
func (m Model) titleCmd() tea.Cmd {
	return tea.SetWindowTitle(windowTitle(m.cfg.Cwd, m.presentationState().titleMarker))
}

// IdleTitleSequence returns the escape sequence that restores the idle terminal
// title for cwd. main writes it once Program.Run returns, before the process
// exits, because the model cannot: Bubble Tea returns on SIGINT or SIGTERM
// without running Update, and a panic in Update or View hands back no model at
// all, so neither exit runs the model's own title command. The sequence is the
// one tea.SetWindowTitle writes.
func IdleTitleSequence(cwd string) string {
	return ansi.SetWindowTitle(windowTitle(cwd, ""))
}

// renderTimelineItem is the timeline owner's render callback. It captures the
// current theme, output limit, and tool-output mode, so a toggle or resize is
// reflected in every row the owner renders next.
func (m *Model) renderTimelineItem(it ui.Item, width int) string {
	return ui.RenderItem(m.theme, it, width, m.cfg.OutputLimit, m.toolOutputMode)
}

// appendItem adds a timeline item, enforces the cap, and drops pending calls
// whose tool block was trimmed away. The owner renders the item and keeps the
// cached rows in step; the Update that applied the event syncs it once via
// syncViewport.
func (m *Model) appendItem(it ui.Item) {
	if m.timeline.append(it, m.cfg.MaxTimelineItems, m.renderTimelineItem) > 0 {
		m.dropDetachedPendingCalls()
	}
}

// firstPendingToolIdx returns the index of the first pending (not yet done)
// KindTool item when all trailing items are pending tools, or -1 otherwise.
// This is used to insert assistant messages before pending tool calls when
// cake emits function_call events before message events.
func (m *Model) firstPendingToolIdx() int {
	i := len(m.timeline.items) - 1
	for i >= 0 && m.timeline.items[i].Kind == ui.KindTool && m.timeline.items[i].Tool != nil && !m.timeline.items[i].Tool.Done {
		i--
	}
	i++ // first pending tool, or len(items) if none found
	if i < len(m.timeline.items) {
		return i
	}
	return -1
}

// lastAssistantMarkdown returns the raw markdown text of the most recent
// assistant message on the timeline, or ok=false when no assistant message
// has arrived yet. Content is copied as received from cake, not sanitized:
// the clipboard receives the same markdown source the user sees rendered.
func (m Model) lastAssistantMarkdown() (text string, ok bool) {
	for i := len(m.timeline.items) - 1; i >= 0; i-- {
		it := m.timeline.items[i]
		if it.Kind == ui.KindAssistant && strings.TrimSpace(it.Text) != "" {
			return it.Text, true
		}
	}
	return "", false
}

// lastItemIsReasoning reports whether the newest timeline item is the
// reasoning marker. Consecutive cake.Reasoning events coalesce into a single
// "(thinking)" entry; any other event type ends the burst, and a marker
// trimmed off the timeline simply starts a fresh one.
func (m *Model) lastItemIsReasoning() bool {
	return len(m.timeline.items) > 0 && m.timeline.items[len(m.timeline.items)-1].Kind == ui.KindReasoning
}

// removeItemAt removes one item and its cached rendering. A pending call whose
// tool block was removed is dropped; surviving calls need no adjustment because
// they reference the block, not a slice position. It resolves the live
// compaction start notice once its outcome is known. The owner copies the
// surviving slices so the removed item and its rendered string are released.
func (m *Model) removeItemAt(idx int) {
	if m.timeline.remove(idx) {
		m.dropDetachedPendingCalls()
	}
}

// insertItemAt inserts an item at the given index, shifting subsequent items
// to the right, and updates the render cache. When the timeline is at
// MaxTimelineItems, the oldest items are trimmed first and idx is clamped into
// the surviving slice, so the inserted item is never trimmed.
func (m *Model) insertItemAt(idx int, it ui.Item) {
	// The owner trims before inserting: trimming after the shift would remove
	// the newly inserted item when idx is 0 and the timeline is already at the
	// cap.
	if m.timeline.insert(idx, it, m.cfg.MaxTimelineItems, m.renderTimelineItem) > 0 {
		m.dropDetachedPendingCalls()
	}
}

// toolIndex returns the timeline index of tool, or -1 when the block is no
// longer attached. A pending call holds the block itself, so trimming,
// insertion, removal, and cap eviction cannot retarget it.
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

// pendingTool resolves callID to its attached tool block and index. A call
// whose block has left the timeline is dropped; ok is false so late output
// appends a standalone block instead of mutating detached data.
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

// dropDetachedPendingCalls removes pending entries whose tool block is no
// longer in the timeline. It keeps a trim or removal from pinning detached
// tool payloads; surviving entries keep their identity.
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

// rerenderItem refreshes one item's cached rendering after it mutated in
// place (a tool block receiving its output).
func (m *Model) rerenderItem(idx int) {
	m.timeline.rerender(idx, m.renderTimelineItem)
}

// rerenderToolItems refreshes every tool item's cached rendering while
// reusing the cached strings for all other timeline item kinds.
func (m *Model) rerenderToolItems() {
	m.timeline.rerenderTools(m.renderTimelineItem)
}

// syncViewport pushes the joined render cache into the viewport, keeping the
// view pinned to the bottom if it was there already. Otherwise it restores
// the previous scroll offset so a single item re-render does not move the view.
// It is a no-op unless a mutation marked the cache dirty, so one Update with a
// burst of events costs one SetContent instead of one per event.
//
// A push changes the live region's visible content, so it also arms the settle
// repaint: new timeline rows are padded to the full width, which is exactly the
// content a terminal that measures a glyph wider than charmbracelet/x/ansi can
// wrap, scrolling the screen under the renderer. See ADR 020.
func (m *Model) syncViewport() {
	if !m.timeline.takeDirty() {
		return
	}
	m.contentChanged = true
	atBottom := m.viewport.AtBottom()
	yOffset := m.viewport.YOffset
	m.viewport.SetContent(m.timeline.content())
	if atBottom {
		m.viewport.GotoBottom()
	} else {
		m.viewport.SetYOffset(yOffset)
	}
}

// layout recomputes component sizes from the terminal size and input
// contents. The timeline is re-rendered only when its width changes; height
// changes just resize the viewport window.
func (m *Model) layout() {
	if m.width <= 0 || m.height <= 0 {
		return
	}
	inputHeight := inputHeightFor(m.input.LineCount())
	inputWidth := m.width - composerHorizontalChrome
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
		m.viewport = viewport.New(m.width, vpHeight)
		m.ready = true
		m.timeline.resize(m.width, m.renderTimelineItem)
		return
	}

	atBottom := m.viewport.AtBottom()
	m.viewport.Height = vpHeight
	if m.viewport.Width != m.width {
		m.viewport.Width = m.width
		m.timeline.resize(m.width, m.renderTimelineItem)
		return
	}
	if atBottom {
		m.viewport.GotoBottom()
	}
}

// inputHeightFor returns the composer textarea height for a prompt of n lines:
// at least minInputHeight rows so the composer keeps its shape, at most
// maxInputHeight so a long paste cannot swallow the timeline.
func inputHeightFor(n int) int {
	if n < minInputHeight {
		return minInputHeight
	}
	if n > maxInputHeight {
		return maxInputHeight
	}
	return n
}

// inlineViewportHeight returns the timeline viewport height in inline mode: the
// full-height viewport capped at inlineViewportMax and at half the terminal, so
// the live region below never takes more than half the window and recent
// terminal history stays visible above the REPL. It returns at least 1 so the
// timeline is never zero-height, even on a terminal too short for the cap.
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

// ComposerRows returns the number of rows the composer occupies in the last
// rendered frame: the textarea height plus its borders. In inline mode main
// erases those rows once the program returns, so the terminal is left with the
// timeline region and the shell prompt rather than a stale input box. It is 0
// before the first layout, when no composer has been rendered.
func (m Model) ComposerRows() int {
	if !m.ready {
		return 0
	}
	return inputHeightFor(m.input.LineCount()) + composerVerticalChrome
}
