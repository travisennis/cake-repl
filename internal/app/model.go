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
	Runs             *cake.Registry
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

// CompactionConfig is the resolved `[compaction]` configuration. Bin is the
// external compactor executable, MaxContextTokens is the quality ceiling for
// absolute mode, TriggerTokens is the absolute compaction line, and
// ThresholdPercent/KeepTokens are the percent fallback and retained-tail target.
// Model, Profile, and ReasoningEffort select the summary call; empty values
// fall back to the REPL's -model/-profile and cake's own reasoning default. The
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
// empty values fall back to the REPL's -model/-profile. See ADR 028 and ADR
// 030.
type SuggestionConfig struct {
	Enabled bool
	Bin     string
	Model   string
	Profile string
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

	input    textarea.Model
	timeline viewport.Model
	spin     spinner.Model

	running       bool
	run           *cake.Run
	hydrating     bool
	replayPending bool
	replayRun     *cake.Run
	replayWarn    bool
	sawComplete   bool
	exitAfter     bool
	// compacting is true while the external compactor runs. It gates prompt
	// submission and session-target changes so the next cake process cannot
	// race the compactor's session lock, and it marks the status line/title as
	// busy. compactionCancel stops the child; the result stays in flight until
	// a compactionDoneMsg reaps it, even after a cancel.
	compacting       bool
	compactionCancel context.CancelFunc
	// compactionStart is when the current compactor was launched and
	// compactionElapsed is its whole-second age, refreshed from the spinner tick
	// the REPL already runs while busy. Both are zero when no compaction is
	// pending, so the status line and composer omit the age. See ADR 026.
	compactionStart   time.Time
	compactionElapsed time.Duration
	// compactionSeq allocates a request identity for one compaction and
	// compactionReq is the identity whose result may still be applied. A
	// canceled or superseded run cannot touch the status line or context of a
	// later one.
	compactionSeq uint64
	compactionReq uint64
	// newSessionPending is true while an old run canceled by Ctrl+N is being
	// drained. Its remaining events and cancellation result belong before the
	// new-session boundary and must not affect the fresh state. It also covers
	// a canceled compaction being drained after Ctrl+N.
	newSessionPending bool

	// Suggestion state (ADR 028). At most one read-only suggestion request is in
	// flight; suggestionSeq allocates a request identity that never repeats, and
	// suggestionReq is the identity whose result may still be applied. A draft,
	// a new turn, or a session change invalidates the identity so a late result
	// cannot overwrite the composer. suggestionText is the currently shown ghost
	// suggestion; empty means none. suggestionWarned keeps a broken suggester to
	// one visible warning per REPL process.
	suggesting       bool
	suggestionSeq    uint64
	suggestionReq    uint64
	suggestionCancel context.CancelFunc
	// suggestionDone closes when the suggester child has exited and been reaped.
	// It stays set after cancelSuggestion invalidates the request, so quit and
	// main's unobserved-exit teardown can wait for the child instead of letting
	// it outlive the REPL.
	suggestionDone   chan struct{}
	suggestionText   string
	suggestionWarned bool
	// pendingSuggestion holds a usable result that arrived while compaction
	// still gated the composer. pendingSuggestionSession is the session the
	// request named, so the held text cannot cross a session boundary. Both are
	// cleared by every path that dismisses a visible ghost. See ADR 031.
	pendingSuggestion        string
	pendingSuggestionSession string

	session      sessionState
	history      promptHistory
	pendingCalls map[string]int // call_id -> index into items
	items        []ui.Item
	// toolOutputMode applies to every tool block in the timeline and to tool
	// blocks appended later in this REPL session.
	toolOutputMode ui.ToolOutputMode

	// rendered caches one rendered string per item at renderedWidth and is the
	// source of truth for the viewport payload: syncViewport joins it with a
	// double newline once per Update. timelineDirty records whether a mutation
	// since the last sync needs a viewport push, so a burst of events costs one
	// viewport.SetContent per Update instead of one per event. SetContent still
	// re-parses the whole payload, so per-event cost stays linear in the
	// timeline size; coalescing only removes the per-event constant.
	rendered      []string
	renderedWidth int
	timelineDirty bool

	// contentChanged is set by the updates that reshape the live region's
	// visible content (a viewport push, a composer key) and read by the settle
	// sweep, which repaints the whole frame after it; see ADR 020.
	contentChanged bool

	// completion state for Tab cycling; zero values mean no active cycle.
	completionPrefix  string
	completionMatches []string
	completionIdx     int

	// Session browser overlay state, driven by the /sessions command or, at
	// startup, by the picker a bare `-resume` requests. The browser is idle-only
	// and replaces the timeline while open.
	browserOpen    bool
	browserLoading bool
	browserErr     string
	sessionList    list.Model

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
		cfg:           cfg,
		theme:         th,
		keys:          defaultKeyMap(),
		input:         input,
		spin:          spin,
		sessionList:   newSessionList(),
		pendingCalls:  map[string]int{},
		clipboard:     clipboard.WriteAll,
		hydrating:     cfg.ResumeID != "",
		replayPending: cfg.ResumeID != "",
		session: sessionState{
			NextMode: cfg.InitialMode,
			ResumeID: cfg.ResumeID,
		},
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

// CancelRunning cancels any active cake subprocess. Safe to call when
// nothing is running.
func (m Model) CancelRunning() {
	if m.run != nil {
		m.run.Cancel()
	}
	if m.replayRun != nil {
		m.replayRun.Cancel()
	}
	m.cancelCompaction()
	// A suggestion is read-only, but it is still a child the unobserved exit
	// path must stop; no result may arrive after the program has returned. Wait
	// for the child to be reaped because the Update loop that would normally
	// process its result is already gone.
	m.cancelSuggestion()
	// A session listing can be in flight without the model ever seeing a key:
	// the startup picker starts one before the first frame, and main calls this
	// for an exit the Update loop never observed (SIGINT, SIGTERM, a recovered
	// panic), which is exactly the path where quit() cannot cancel it. Cancel it
	// before the suggestion wait so it is not held behind that child.
	m.cancelSessionList()
	if done := m.suggestionDone; done != nil {
		timer := time.NewTimer(suggestionTeardownTimeout)
		select {
		case <-done:
			timer.Stop()
		case <-timer.C:
		}
	}
}

// cancelCompaction stops an in-flight compactor. The process keeps the same
// result identity until its compactionDoneMsg arrives, so the gate stays closed
// until the child has actually exited and been reaped.
func (m *Model) cancelCompaction() {
	if m.compactionCancel != nil {
		m.compactionCancel()
		m.compactionCancel = nil
	}
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
	if m.startupCmd != nil {
		cmds = append(cmds, m.startupCmd)
	}
	if m.hydrating {
		cmds = append(cmds, startReplayCmd(m.cfg))
	}
	return tea.Batch(cmds...)
}

// windowTitle renders the terminal title for a working directory and run state.
// It is the single place the title format is defined, so the idle form, the
// working form, the compacting form, and the sequence main writes on an
// unobserved exit cannot drift apart.
func windowTitle(cwd string, running, compacting bool) string {
	title := "cake-repl: " + ui.Sanitize(cwd)
	switch {
	case running:
		title = "[working] " + title
	case compacting:
		title = "[compacting] " + title
	}
	return title
}

// titleCmd returns a command that re-sets the terminal title from the current
// working state. The status line and composer already report running state,
// but neither is visible when the terminal window is unfocused, behind another
// window, or reduced to a tab, so the title carries the same signal: a working
// marker prefixes the documented `cake-repl: <cwd>` title while a task runs and
// is absent when the REPL is idle. Emit this on every busy-state transition
// (a run starting or ending, a compaction starting or ending) so the title never
// describes a state the REPL has left.
func (m Model) titleCmd() tea.Cmd {
	return tea.SetWindowTitle(windowTitle(m.cfg.Cwd, m.running, m.compacting))
}

// IdleTitleSequence returns the escape sequence that restores the idle terminal
// title for cwd. main writes it once Program.Run returns, before the process
// exits, because the model cannot: Bubble Tea returns on SIGINT or SIGTERM
// without running Update, and a panic in Update or View hands back no model at
// all, so neither exit runs the model's own title command. The sequence is the
// one tea.SetWindowTitle writes.
func IdleTitleSequence(cwd string) string {
	return ansi.SetWindowTitle(windowTitle(cwd, false, false))
}

// trimFront removes the first `over` items from the timeline, adjusts
// pendingCalls indices, and drops entries for trimmed-away pending calls. The
// survivors are copied into a fresh slice so the trimmed items' payloads
// (tool outputs, assistant text) are released: reslicing would keep them
// reachable through the old backing array, so -max-timeline-items would bound
// the visible timeline but not the memory it pins.
func (m *Model) trimFront(over int) {
	kept := make([]ui.Item, len(m.items)-over)
	copy(kept, m.items[over:])
	m.items = kept
	for callID, callIdx := range m.pendingCalls {
		if callIdx < over {
			delete(m.pendingCalls, callID)
		} else {
			m.pendingCalls[callID] = callIdx - over
		}
	}
}

// appendItem adds a timeline item, renders it into the cache, and returns
// its index. It marks the viewport payload dirty; the Update that applied the
// event syncs it once via syncViewport.
func (m *Model) appendItem(it ui.Item) int {
	m.items = append(m.items, it)
	if m.cfg.MaxTimelineItems > 0 && len(m.items) > m.cfg.MaxTimelineItems {
		over := len(m.items) - m.cfg.MaxTimelineItems
		m.trimFront(over)
		if m.ready {
			// Copy the surviving renders too: reslicing would keep the old
			// backing array (and the trimmed strings) reachable. The O(n) copy
			// per trim over the cap is the cost of actually releasing the
			// trimmed entries.
			kept := make([]string, len(m.rendered)-over)
			copy(kept, m.rendered[over:])
			m.rendered = kept
			m.timelineDirty = true
		}
	}
	if m.ready {
		m.appendTimelineContent(ui.RenderItem(m.theme, it, m.renderedWidth, m.cfg.OutputLimit, m.toolOutputMode))
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

// lastAssistantMarkdown returns the raw markdown text of the most recent
// assistant message on the timeline, or ok=false when no assistant message
// has arrived yet. Content is copied as received from cake, not sanitized:
// the clipboard receives the same markdown source the user sees rendered.
func (m Model) lastAssistantMarkdown() (text string, ok bool) {
	for i := len(m.items) - 1; i >= 0; i-- {
		it := m.items[i]
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
	return len(m.items) > 0 && m.items[len(m.items)-1].Kind == ui.KindReasoning
}

// removeItemAt removes one item and its cached rendering, adjusting pending
// call indices the same way trimFront and insertItemAt do. It resolves the live
// compaction start notice once its outcome is known. The surviving slices are
// copied so the removed item and its rendered string are not kept reachable
// through the old backing arrays.
func (m *Model) removeItemAt(idx int) {
	if idx < 0 || idx >= len(m.items) {
		return
	}
	kept := make([]ui.Item, len(m.items)-1)
	copy(kept, m.items[:idx])
	copy(kept[idx:], m.items[idx+1:])
	m.items = kept

	for callID, callIdx := range m.pendingCalls {
		switch {
		case callIdx == idx:
			delete(m.pendingCalls, callID)
		case callIdx > idx:
			m.pendingCalls[callID] = callIdx - 1
		}
	}

	if !m.ready {
		return
	}
	if len(m.rendered) == len(m.items)+1 {
		keptRenders := make([]string, len(m.rendered)-1)
		copy(keptRenders, m.rendered[:idx])
		copy(keptRenders[idx:], m.rendered[idx+1:])
		m.rendered = keptRenders
		m.timelineDirty = true
		return
	}
	// The cache was not in step with items (an item appended before the first
	// layout); rebuild it rather than leave a removed row in the payload.
	m.rebuildTimeline()
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
			// Drop the render cache before rebuilding: rebuild reslices to
			// length zero, which would otherwise keep the pre-trim strings
			// reachable through the old backing array.
			m.rendered = nil
			m.rebuildTimeline()
		}
		return
	}

	if m.ready {
		rendered := ui.RenderItem(m.theme, it, m.renderedWidth, m.cfg.OutputLimit, m.toolOutputMode)
		m.rendered = append(m.rendered, "") // make room
		copy(m.rendered[idx+1:], m.rendered[idx:])
		m.rendered[idx] = rendered
		m.timelineDirty = true
	}
}

// rerenderItem refreshes one item's cached rendering after it mutated in
// place (a tool block receiving its output).
func (m *Model) rerenderItem(idx int) {
	if !m.ready || idx < 0 || idx >= len(m.rendered) {
		return
	}
	m.rendered[idx] = ui.RenderItem(m.theme, m.items[idx], m.renderedWidth, m.cfg.OutputLimit, m.toolOutputMode)
	m.timelineDirty = true
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
	m.timelineDirty = true
}

// rebuildTimeline re-renders every item at the current viewport width. Width
// changes and /clear use this; other changes update the cache incrementally.
func (m *Model) rebuildTimeline() {
	if !m.ready {
		return
	}
	m.renderedWidth = m.timeline.Width
	if len(m.items) == 0 {
		// /clear and Ctrl+N empty the timeline; drop the backing array too, or
		// reslicing to zero would keep every old rendered string pinned.
		m.rendered = nil
		m.timelineDirty = true
		return
	}
	m.rendered = m.rendered[:0]
	for _, it := range m.items {
		m.rendered = append(m.rendered, ui.RenderItem(m.theme, it, m.renderedWidth, m.cfg.OutputLimit, m.toolOutputMode))
	}
	m.timelineDirty = true
}

// appendTimelineContent extends the render cache with one item's rendering.
// The viewport payload is joined lazily at sync time, so appending never
// copies the accumulated timeline string.
func (m *Model) appendTimelineContent(rendered string) {
	m.rendered = append(m.rendered, rendered)
	m.timelineDirty = true
}

// rebuildTimelineContent returns the viewport payload: every cached rendering
// joined with a double newline. syncViewport recomputes it once per Update.
func (m *Model) rebuildTimelineContent() string {
	return strings.Join(m.rendered, "\n\n")
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
	if !m.timelineDirty {
		return
	}
	m.timelineDirty = false
	m.contentChanged = true
	atBottom := m.timeline.AtBottom()
	yOffset := m.timeline.YOffset
	m.timeline.SetContent(m.rebuildTimelineContent())
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
