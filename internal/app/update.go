package app

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/travisennis/cake-repl/internal/cake"
	"github.com/travisennis/cake-repl/internal/ui"
)

// maxEventsPerBatch bounds how many stream events one Update applies, so a
// fast cake stream cannot starve the event loop (spinner, key handling).
const maxEventsPerBatch = 10

// settleDelay is the sweep interval for the settle repaint: at most one full
// frame rewrite per interval, however many content changes land in it. See
// ADR 020.
const settleDelay = 300 * time.Millisecond

// maxRetainedToolOutputBytes is the hard ceiling on how much of one tool
// result the timeline keeps in memory. It is deliberately far above the
// render-time output-limit (default 2000 bytes): the limit decides what is
// drawn, while this bound decides what the session pins, so Ctrl+O full mode
// stays useful for realistic output without letting a single oversized result
// hold tens of MB for the life of the session. Output beyond the ceiling is
// cut at ingest with the standard truncation marker.
const maxRetainedToolOutputBytes = 1 << 20 // 1 MiB

// capRetainedToolOutput applies the retention ceiling to one tool result at
// ingest by delegating to [ui.CapOutputBytes], which owns both branches: at or
// below the ceiling the output passes through byte-identical (so Ctrl+O full
// mode is unchanged for realistic results), and above it the output is cut on a
// rune boundary and appended with the standard
// "… truncated (N bytes total)" marker, where N is the ORIGINAL raw byte count.
//
// The oversized decision and the marker use the result's raw bytes, and the
// oversized branch returns a fresh, bounded copy — never a substring of the
// original. It must not delegate to [ui.TruncateOutput], which trims trailing
// newlines before its size check and then returns the shortened string
// directly: for a short prefix followed by megabytes of newlines that trimmed
// result is a few-byte substring still backed by the entire original
// allocation, so the timeline silently pinned tens of MB with no marker (task
// 098). [ui.CapOutputBytes] keeps the newline trimming at display time.
func capRetainedToolOutput(output string) string {
	return ui.CapOutputBytes(output, maxRetainedToolOutputBytes)
}

// eventsMsg delivers a batch of decoded cake stream events, applied in order
// with a single viewport sync at the end of the Update.
type eventsMsg struct{ evs []cake.Event }

// runDoneMsg delivers the terminal state of a cake subprocess.
type runDoneMsg struct{ res cake.Result }

// clipboardResultMsg delivers the outcome of one off-loop clipboard write. It
// deliberately carries no payload text: a result can only become the existing
// info/error feedback, so clipboard content has no new path into a log or the
// timeline. sessionID marks the /session copy wording (the fixed id notice)
// versus the assistant copy, which reports the snapshotted rune count.
type clipboardResultMsg struct {
	sessionID bool
	chars     int
	err       error
}

// waitForRun pulls the next batch of events from a run, or its final result
// once the event stream is closed. It is re-issued after every batch.
func waitForRun(run *cake.Run) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-run.Events
		if !ok {
			return runDoneMsg{res: <-run.Result}
		}
		evs := []cake.Event{ev}
		// Drain the events already queued behind the first one so a burst of K
		// events costs one Update (and one viewport sync) instead of K. The
		// bound keeps a single Update from starving the event loop.
		for len(evs) < maxEventsPerBatch {
			select {
			case ev, ok := <-run.Events:
				if !ok {
					// The stream closed mid-drain; the next waitForRun delivers
					// runDoneMsg.
					return eventsMsg{evs: evs}
				}
				evs = append(evs, ev)
			default:
				return eventsMsg{evs: evs}
			}
		}
		return eventsMsg{evs: evs}
	}
}

// sessionsLoadedMsg delivers the result of one read-only session listing. req
// is the request identity the result belongs to; a result whose identity no
// longer matches the active request is superseded and dropped.
type sessionsLoadedMsg struct {
	req      uint64
	sessions []cake.SessionSummary
	err      error
}

// startSessionListLoad cancels any in-flight listing and starts a fresh
// read-only listing under a new request identity. The model owns the
// cancellation: closing the browser, replacing the listing, or quitting cancels
// it, and only the newest request may apply its result. The command runs off the
// Update loop, so a slow or missing cake binary cannot block key handling.
//
// It returns the updated model and the command as a pair rather than mutating a
// receiver a caller reads in a sibling return operand: that ordering is
// unspecified by the language.
func (m Model) startSessionListLoad() (Model, tea.Cmd) {
	m.cancelSessionList()
	m.sessionListSeq++
	m.sessionListReq = m.sessionListSeq
	ctx, cancel := context.WithCancel(context.Background())
	m.sessionListCancel = cancel
	req := m.sessionListReq
	cfg := m.cfg
	return m, func() tea.Msg {
		sessions, err := cake.ListSessions(ctx, cake.SessionsOptions{
			Bin:      cfg.CakeBin,
			Cwd:      cfg.Cwd,
			DebugLog: cfg.DebugLog,
		})
		return sessionsLoadedMsg{req: req, sessions: sessions, err: err}
	}
}

// replayStartedMsg reports the subprocess created for startup hydration.
type replayStartedMsg struct {
	run *cake.Run
	err error
}

// replayEventsMsg delivers a batch of replay records in stream order.
type replayEventsMsg struct{ evs []cake.Event }

// replayDoneMsg delivers the terminal state of a replay subprocess.
type replayDoneMsg struct{ res cake.Result }

// startReplayCmd starts the read-only replay without making the live prompt
// state look busy. The command runs outside the Update loop because starting a
// subprocess can block briefly.
func startReplayCmd(cfg Config) tea.Cmd {
	return func() tea.Msg {
		run, err := cake.Replay(cake.ReplayOptions{
			Bin:       cfg.CakeBin,
			Cwd:       cfg.Cwd,
			SessionID: cfg.ResumeID,
			DebugLog:  cfg.DebugLog,
			Registry:  cfg.Runs,
		})
		return replayStartedMsg{run: run, err: err}
	}
}

// waitForReplay pulls the next batch of events from a replay run, or its final
// result once the event stream is closed. It mirrors waitForRun but keeps
// hydration separate from a live prompt run.
func waitForReplay(run *cake.Run) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-run.Events
		if !ok {
			return replayDoneMsg{res: <-run.Result}
		}
		evs := []cake.Event{ev}
		for len(evs) < maxEventsPerBatch {
			select {
			case ev, ok := <-run.Events:
				if !ok {
					return replayEventsMsg{evs: evs}
				}
				evs = append(evs, ev)
			default:
				return replayEventsMsg{evs: evs}
			}
		}
		return replayEventsMsg{evs: evs}
	}
}

// settleSweepMsg is the periodic tick that decides whether the frame needs a
// settle repaint; see ADR 020.
type settleSweepMsg struct{}

// settleSweepCmd schedules the next settle sweep. Every sweep arms the one
// after it, so the sweep runs for the life of the program at settleDelay.
func settleSweepCmd() tea.Cmd {
	return tea.Tick(settleDelay, func(time.Time) tea.Msg { return settleSweepMsg{} })
}

// settleSweepAtStartupCmd returns the first settle sweep without waiting, so
// startup pays no timer and a test that runs Init's commands does not block.
// That first sweep arms the delayed chain from its handler.
func settleSweepAtStartupCmd() tea.Cmd {
	return func() tea.Msg { return settleSweepMsg{} }
}

// settleSweep handles one sweep: when content has changed since the last
// sweep, ask the program to rewrite the whole frame, and always arm the next
// sweep.
//
// Bubble Tea's renderer rewrites only the lines whose text changed since the
// previous frame (standard_renderer.flush compares lastRenderedLines). A
// terminal that measures a glyph wider than charmbracelet/x/ansi does — a
// text-default emoji such as U+261D is one cell to x/ansi and two to
// utf8proc-based terminals — wraps the full-width row that contains it, which
// makes the frame one physical row taller than the screen and scrolls the whole
// frame up by one. The renderer cannot see that scroll, so the lines it skips
// keep whatever the scrolled screen moved into their place: that is how a run
// left a stale composer border and a previous status line on screen in the task
// 110 report.
//
// The repair is the one a resize already performs: report the current size to
// the program, which makes the renderer drop the line cache it diffs against
// (repaint) so the next flush rewrites every row in order. Doing that per
// change would rewrite the frame per event, so the sweep coalesces: whatever
// changed inside the interval is covered by one repaint, and an idle interval
// writes nothing at all.
func (m Model) settleSweep() (Model, tea.Cmd) {
	next := settleSweepCmd()
	if !m.ready || !m.contentChanged {
		return m, next
	}
	m.contentChanged = false
	if repaint := m.repaintCmd(); repaint != nil {
		return m, tea.Batch(repaint, next)
	}
	return m, next
}

// repaintCmd reports the model's current size to the program. The renderer
// treats any WindowSizeMsg as a repaint — it drops the line cache it diffs
// against — and the model's own handler for the same message re-lays out at the
// same size, which is a no-op apart from flushing a dirty viewport. Nothing on
// screen changes: the message only makes the next flush rewrite every row of
// the frame instead of the rows whose text changed.
func (m Model) repaintCmd() tea.Cmd {
	if m.width <= 0 || m.height <= 0 {
		return nil
	}
	w, h := m.width, m.height
	return func() tea.Msg { return tea.WindowSizeMsg{Width: w, Height: h} }
}

// Update implements tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case settleSweepMsg:
		return m.settleSweep()

	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.layout()
		m.syncViewport()
		return m, nil

	case spinner.TickMsg:
		if !m.running && !m.compacting {
			return m, nil
		}
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		if m.compacting {
			// The spinner tick is already flowing while a task or compaction is
			// in flight, so it also refreshes the elapsed-seconds display without
			// starting a second timer chain. Use the tick's own time when present
			// so a test can advance it deterministically.
			now := msg.Time
			if now.IsZero() {
				now = time.Now()
			}
			m.refreshCompactionElapsed(now)
		}
		return m, cmd

	case compactionDoneMsg:
		return m.finishCompaction(msg)

	case suggestionDoneMsg:
		next := m.finishSuggestion(msg)
		if next.exitAfter {
			// quit() canceled the in-flight suggester and is waiting for this
			// result to reap the child before returning the quit command.
			return next.quit()
		}
		return next, nil

	case replayStartedMsg:
		if msg.err != nil {
			if m.replayPending {
				m.replayPending = false
				if m.hydrating {
					m.hydrating = false
					m.replayWarn = true
					m.appendItem(ui.Item{Kind: ui.KindWarning, Text: "session replay unavailable: " + msg.err.Error()})
				}
			}
			// A startup failure is promised to be visibly non-fatal (ADR 011),
			// so push the warning into the viewport now instead of leaving it
			// for the next key or resize to flush.
			m.syncViewport()
			if m.exitAfter {
				return m.quit()
			}
			return m, nil
		}
		m.replayRun = msg.run
		if m.exitAfter || !m.hydrating {
			m.replayRun.Cancel()
		}
		return m, waitForReplay(m.replayRun)

	case replayEventsMsg:
		if m.hydrating {
			for _, ev := range msg.evs {
				m.applyReplayEvent(ev)
			}
			m.syncViewport()
		}
		if m.replayRun == nil {
			return m, nil
		}
		return m, waitForReplay(m.replayRun)

	case replayDoneMsg:
		return m.finishReplay(msg.res)

	case eventsMsg:
		if !m.newSessionPending {
			for _, ev := range msg.evs {
				m.applyEvent(ev)
			}
		}
		m.syncViewport()
		return m, waitForRun(m.run)

	case runDoneMsg:
		return m.finishRun(msg.res)

	case clipboardResultMsg:
		if msg.err != nil {
			m.appendItem(ui.Item{Kind: ui.KindError, Text: "copy failed: " + msg.err.Error()})
			m.syncViewport()
			return m, nil
		}
		var text string
		if msg.sessionID {
			text = "copied session id to clipboard"
		} else {
			text = fmt.Sprintf("copied %d chars to clipboard", msg.chars)
		}
		m.appendItem(ui.Item{Kind: ui.KindInfo, Text: text})
		m.syncViewport()
		return m, nil

	case sessionsLoadedMsg:
		if msg.req != m.sessionListReq {
			// A superseded or invalidated request: its items, error, selection,
			// and loading state must not touch the request the browser is on
			// now.
			return m, nil
		}
		m.browserLoading = false
		m.sessionListCancel = nil
		if msg.err != nil {
			m.browserErr = ui.TruncateOutput(msg.err.Error(), m.cfg.OutputLimit)
			// The rows on screen are gone, so the list must not keep the previous
			// listing selectable behind the error.
			m.resetSessionList()
			return m, nil
		}
		m.browserErr = ""
		now := time.Now()
		items := make([]list.Item, 0, len(msg.sessions))
		for _, s := range msg.sessions {
			items = append(items, newSessionItem(s, now))
		}
		m.sessionList.SetItems(items)
		m.sessionList.ResetSelected()
		return m, nil

	case tea.MouseMsg:
		// Only forward wheel events to the timeline viewport; let other mouse
		// events (clicks in the input area) fall through to the default handler.
		if msg.Action == tea.MouseActionPress &&
			(msg.Button == tea.MouseButtonWheelUp || msg.Button == tea.MouseButtonWheelDown) {
			var cmd tea.Cmd
			m.timeline, cmd = m.timeline.Update(msg)
			return m, cmd
		}

	case tea.KeyMsg:
		mm, cmd := m.handleKey(msg)
		m = mm.(Model)
		m.syncViewport()
		// A key can change the composer's visible text — and with it the frame's
		// shape — so it arms the settle repaint like a viewport push does. Cursor
		// blinks and mouse clicks do not come through here and leave it alone.
		m.contentChanged = true
		return m, cmd
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

// quit cancels any in-flight child and then quits, so the model-owned
// operation stops before the program exits. An in-flight suggester is drained:
// the quit waits for its result, which reaps the child, before returning
// tea.Quit. It returns the updated model with the quit command, so no caller
// reads the model in a sibling return operand whose order relative to this call
// is unspecified.
func (m Model) quit() (Model, tea.Cmd) {
	m.cancelCompaction()
	m.cancelSessionList()
	m.cancelSuggestion()
	if m.suggestionDone != nil {
		if !m.exitAfter {
			m.exitAfter = true
			m.appendItem(ui.Item{Kind: ui.KindWarning, Text: "canceling prompt suggestion, then exiting…"})
		}
		m.syncViewport()
		return m, nil
	}
	return m, tea.Quit
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.exitAfter {
		// The REPL is draining a child before it exits. A submission or session
		// change here would start new work behind the quit.
		return m, nil
	}
	if m.browserOpen {
		return m.handleBrowserKey(msg)
	}
	switch {
	case key.Matches(msg, m.keys.CancelQuit):
		if m.running {
			m.run.Cancel()
			return m, nil
		}
		if m.compacting {
			// Keep the compaction gate closed until the child exits and the
			// result is reaped, so the next prompt cannot race its session lock.
			// The reaped result supplies the one "canceled" notice, matching a
			// canceled run, so no second notice is added here.
			m.cancelCompaction()
			return m, nil
		}
		if m.hydrating || m.replayPending {
			m.exitAfter = true
			if m.replayRun != nil {
				m.replayRun.Cancel()
			}
			return m, nil
		}
		return m.quit()

	case key.Matches(msg, m.keys.Submit):
		return m.submit()

	case key.Matches(msg, m.keys.NewSession):
		return m.startNewSession()

	case key.Matches(msg, m.keys.ClearInput):
		m.clearSuggestion()
		m.input.Reset()
		m.layout()
		return m, nil

	case key.Matches(msg, m.keys.PageUp), key.Matches(msg, m.keys.PageDown):
		var cmd tea.Cmd
		m.timeline, cmd = m.timeline.Update(msg)
		return m, cmd

	case key.Matches(msg, m.keys.TabComplete):
		return m.handleTabComplete()

	case key.Matches(msg, m.keys.ToggleToolOutput):
		return m.toggleToolOutput()

	case key.Matches(msg, m.keys.CopyLastAssistant):
		return m.copyLastAssistant()

	// Up/down recall prompt history only at the input's edge; inside a
	// multi-line input they keep moving the cursor.
	case key.Matches(msg, m.keys.HistoryPrev):
		if m.input.Line() == 0 {
			m.clearSuggestion()
			if text, ok := m.history.Prev(m.input.Value()); ok {
				m.input.SetValue(text)
				m.layout()
				return m, nil
			}
		}

	case key.Matches(msg, m.keys.HistoryNext):
		if m.input.Line() == m.input.LineCount()-1 {
			m.clearSuggestion()
			if text, ok := m.history.Next(); ok {
				m.input.SetValue(text)
				m.layout()
				return m, nil
			}
		}
	}

	// Any key that reaches here modifies the input — reset the completion cycle
	// and dismiss a ghost suggestion before the character is inserted, so a
	// suggestion is never appended to by the first typed word.
	m.clearSuggestion()
	m.completionPrefix = ""
	m.completionMatches = nil
	m.completionIdx = 0

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	m.layout() // input height tracks line count
	return m, cmd
}

// handleBrowserKey routes keys while the /sessions browser is open. It returns
// before the composer sees the message, so the hidden input cannot swallow
// navigation. Esc, q, and Ctrl+C close the browser without action; Enter pins
// the next prompt to the session the user can actually see selected.
func (m Model) handleBrowserKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "q", "ctrl+c":
		m.closeBrowser()
		return m, nil
	case "enter":
		// Only a row from the current successful listing is selectable: while a
		// reload is in flight, after a failed reload, or on an empty listing the
		// browser shows no list, so Enter is a no-op instead of resuming an
		// invisible row from a previous visit.
		if !m.browserSelectable() {
			return m, nil
		}
		if it, ok := m.sessionList.SelectedItem().(sessionItem); ok {
			m.closeBrowser()
			return m.resumeSession(it.summary.SessionID)
		}
		return m, nil
	case "up", "down", "pgup", "pgdown", "home", "end":
		var cmd tea.Cmd
		m.sessionList, cmd = m.sessionList.Update(msg)
		return m, cmd
	}
	return m, nil
}

// startNewSession creates an immediate visual and session-state boundary.
// When a live run, replay, or compaction is active, its process is canceled and
// drained normally, but its remaining events are suppressed so they cannot enter
// or re-pin the new session.
func (m Model) startNewSession() (tea.Model, tea.Cmd) {
	m.clearSuggestion()
	if m.running {
		m.newSessionPending = true
		m.run.Cancel()
	}
	if m.compacting {
		m.newSessionPending = true
		m.cancelCompaction()
	}
	if m.hydrating {
		if m.replayRun != nil {
			m.replayRun.Cancel()
		}
		// Keep consuming the canceled replay in the background, but suppress its
		// records after this new-session boundary.
		m.hydrating = false
	}
	m.session.Reset()
	m.sawComplete = false
	m.replayWarn = false
	m.items = nil
	m.pendingCalls = map[string]int{}
	m.rebuildTimeline()
	m.appendItem(ui.Item{Kind: ui.KindInfo, Text: "New session"})
	return m, nil
}

// toggleToolOutput cycles the session-wide tool output mode and re-renders
// only tool items, reusing cached renders for every other item kind.
func (m Model) toggleToolOutput() (tea.Model, tea.Cmd) {
	m.toolOutputMode = m.toolOutputMode.Cycle()
	m.rerenderToolItems()
	return m, nil
}

// clipboardCmd runs one clipboard write off the Update loop. The caller
// snapshots text and the writer at request time, so the copy target is exactly
// what the user asked for — never text from a later timeline or session — and a
// slow or stuck platform helper (atotto/clipboard waits on the helper with no
// timeout) cannot freeze the single Update loop: cancel, quit, the spinner, and
// stream consumption all keep flowing while the write is pending.
func (m Model) clipboardCmd(text string, sessionID bool) tea.Cmd {
	write := m.clipboard
	chars := utf8.RuneCountInString(text)
	return func() tea.Msg {
		return clipboardResultMsg{sessionID: sessionID, chars: chars, err: write(text)}
	}
}

// copyLastAssistant snapshots the raw markdown of the most recent assistant
// message and copies it on a command, reporting the outcome on the timeline so
// the action always has visible feedback. The write runs off the Update loop so
// a stuck helper cannot block the REPL. Like persistHistory it is a one-shot
// best-effort write: a failure appends an error item instead of interrupting
// the REPL. Finding no assistant response warns immediately and never invokes
// the helper.
func (m Model) copyLastAssistant() (tea.Model, tea.Cmd) {
	text, ok := m.lastAssistantMarkdown()
	if !ok {
		m.appendItem(ui.Item{Kind: ui.KindWarning, Text: "no assistant response to copy"})
		return m, nil
	}
	return m, m.clipboardCmd(text, false)
}

// copySessionID copies the full cake session UUID to the system clipboard so
// the id can be reused for -resume or -fork without leaving the REPL. The
// status line and timeline only show it shortened; this is the on-demand way
// to get the whole value. The id comes from SessionData, so it honors the
// resume pin before the first task reports one. Like copyLastAssistant the
// write runs as a command, one-shot and best-effort, with visible feedback; an
// unknown id warns immediately and never invokes the helper.
func (m Model) copySessionID() (tea.Model, tea.Cmd) {
	id, _ := m.SessionData()
	if id == "" {
		m.appendItem(ui.Item{Kind: ui.KindWarning, Text: "no session id to copy"})
		return m, nil
	}
	return m, m.clipboardCmd(id, true)
}

// handleTabComplete implements Tab key completion with cycling.
func (m Model) handleTabComplete() (tea.Model, tea.Cmd) {
	// Tab is an input action; a suggestion is a one-key hint, not a prefix to
	// complete. Dismiss it before the completer reads the (empty) input.
	m.clearSuggestion()
	input := m.input.Value()

	// If the input changed since the last Tab press, start a fresh cycle.
	// Compare against what the completer last set (completionMatches[completionIdx])
	// rather than the original prefix, so that pressing Tab again advances the cycle.
	if m.completionPrefix != "" && input != m.completionMatches[m.completionIdx] {
		m.completionPrefix = ""
		m.completionMatches = nil
		m.completionIdx = 0
	}

	known := []string{}
	if m.session.SessionID != "" {
		known = append(known, m.session.SessionID)
	}
	if m.session.ResumeID != "" {
		known = append(known, m.session.ResumeID)
	}

	// No active cycle — compute matches from scratch.
	if m.completionPrefix == "" {
		matches, ok := completeSlash(input, known)
		if !ok {
			return m, nil
		}
		if len(matches) == 1 {
			// Single match: complete directly, no cycle needed.
			m.input.SetValue(matches[0])
			m.layout()
			return m, nil
		}
		// Multiple matches: start cycling.
		m.completionPrefix = input
		m.completionMatches = matches
		m.completionIdx = 0
		m.input.SetValue(matches[0])
		m.layout()
		return m, nil
	}

	// Advance the existing cycle.
	m.completionIdx++
	if m.completionIdx >= len(m.completionMatches) {
		// Wrap around: restore the original prefix and end the cycle.
		m.input.SetValue(m.completionPrefix)
		m.completionPrefix = ""
		m.completionMatches = nil
		m.completionIdx = 0
		m.layout()
		return m, nil
	}

	m.input.SetValue(m.completionMatches[m.completionIdx])
	m.layout()
	return m, nil
}

func (m Model) submit() (tea.Model, tea.Cmd) {
	if m.exitAfter {
		// The quit path is waiting for a child to be reaped. Never start a run or
		// execute a command behind it.
		return m, nil
	}
	text := strings.TrimSpace(m.input.Value())
	// Ctrl+S on an empty composer accepts the ghost suggestion. The suggestion
	// is model output, not a user-issued command, so it skips slash-command
	// parsing and always starts an ordinary prompt run.
	fromSuggestion := false
	if text == "" && m.suggestionActive() {
		text = m.suggestionText
		fromSuggestion = true
	}
	if text == "" {
		return m, nil
	}

	if !fromSuggestion {
		if cmd, ok, err := ParseCommand(text); ok {
			if m.history.Add(text) && m.persistHistory(text) {
				m.appendItem(ui.Item{Kind: ui.KindWarning, Text: historyTooLargeWarning()})
			}
			m.input.Reset()
			m.clearSuggestion()
			m.layout()
			if err != nil {
				m.appendItem(ui.Item{Kind: ui.KindError, Text: err.Error()})
				return m, nil
			}
			return m.execCommand(cmd)
		}
	}

	if m.hydrating || m.replayPending || m.replayRun != nil {
		m.appendItem(ui.Item{Kind: ui.KindWarning, Text: "waiting for session history to load…"})
		return m, nil
	}

	if m.newSessionPending {
		m.appendItem(ui.Item{Kind: ui.KindWarning, Text: "waiting for the canceled operation to exit…"})
		return m, nil
	}

	if m.compacting {
		m.appendItem(ui.Item{Kind: ui.KindWarning, Text: "context compaction is running — wait for it or press Ctrl+C to cancel"})
		return m, nil
	}

	if m.running {
		m.appendItem(ui.Item{Kind: ui.KindWarning, Text: "a task is already running — Ctrl+C cancels it"})
		return m, nil
	}
	return m.startRun(text)
}

// needsIdle reports whether a command may only run against an idle REPL: it
// either targets a session (or, for the browser, drives session selection) or
// would race a running subprocess.
func needsIdle(kind CommandKind) bool {
	return kind == CmdNew || kind == CmdResume || kind == CmdSessions
}

func (m Model) execCommand(cmd Command) (tea.Model, tea.Cmd) {
	if (m.hydrating || m.replayPending || m.replayRun != nil) && needsIdle(cmd.Kind) {
		m.appendItem(ui.Item{Kind: ui.KindWarning, Text: "wait for session history to load first"})
		return m, nil
	}
	if m.running && needsIdle(cmd.Kind) {
		m.appendItem(ui.Item{Kind: ui.KindWarning, Text: "finish or cancel the running task first"})
		return m, nil
	}
	if m.compacting && needsIdle(cmd.Kind) {
		m.appendItem(ui.Item{Kind: ui.KindWarning, Text: "wait for context compaction to finish (Ctrl+C cancels it)"})
		return m, nil
	}

	switch cmd.Kind {
	case CmdHelp:
		m.appendItem(ui.Item{Kind: ui.KindInfo, Text: HelpText})

	case CmdExit:
		if m.compacting {
			m.exitAfter = true
			m.cancelCompaction()
			m.appendItem(ui.Item{Kind: ui.KindWarning, Text: "canceling context compaction, then exiting…"})
			return m, nil
		}
		if m.running {
			m.exitAfter = true
			m.run.Cancel()
			m.appendItem(ui.Item{Kind: ui.KindWarning, Text: "canceling task, then exiting…"})
			return m, nil
		}
		if m.hydrating || m.replayPending {
			m.exitAfter = true
			if m.replayRun != nil {
				m.replayRun.Cancel()
			}
			m.appendItem(ui.Item{Kind: ui.KindWarning, Text: "canceling session history load, then exiting…"})
			return m, nil
		}
		return m.quit()

	case CmdNew:
		m.clearSuggestion()
		m.session.Reset()
		m.appendItem(ui.Item{Kind: ui.KindInfo, Text: "next prompt starts a fresh cake session"})

	case CmdResume:
		return m.resumeSession(cmd.Arg)

	case CmdSessions:
		return m.openSessionBrowser()

	case CmdSession:
		if cmd.Arg == "copy" {
			return m.copySessionID()
		}
		m.appendItem(ui.Item{Kind: ui.KindInfo, Text: m.sessionInfo()})

	case CmdClear:
		m.items = nil
		// Indices in pendingCalls point into items; they are invalid now.
		// Late outputs will be appended as standalone blocks instead.
		m.pendingCalls = map[string]int{}
		m.rebuildTimeline()
	}
	return m, nil
}

// resumeSession points the next prompt at id and reports it, mirroring the
// /resume command. The /sessions browser's Enter delegates here so both paths
// share one notice and one state transition.
func (m Model) resumeSession(id string) (tea.Model, tea.Cmd) {
	m.clearSuggestion()
	m.session.UseResume(id)
	m.appendItem(ui.Item{Kind: ui.KindInfo, Text: "next prompt resumes session " + id})
	return m, nil
}

// persistHistory appends one entry to the history file when configured and
// reports whether the entry was too large to record. A record whose encoded
// line would exceed maxHistoryRecordBytes is dropped rather than written,
// because loadHistory could never read it back; the caller may warn on a true
// result but must not echo the prompt text.
//
// Write errors (a bad path or permissions) are silently ignored so they never
// break the REPL; only the size guard is surfaced.
//
// Each entry is stored as "\x02" + JSON-encoded prompt + "\n". The \x02
// prefix unambiguously marks JSONL records so loadHistory can distinguish
// them from legacy plain-text lines. Appending via O_APPEND is safe for
// concurrent writers.
func (m Model) persistHistory(text string) (oversized bool) {
	if m.cfg.HistoryFile == "" {
		return false
	}
	rec := encodeHistoryEntry(text)
	if len(rec) > maxHistoryRecordBytes {
		return true
	}
	f, err := os.OpenFile(m.cfg.HistoryFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return false
	}
	defer f.Close() //nolint:errcheck // best-effort on close
	_, _ = f.WriteString(rec)
	return false
}

func (m Model) startRun(prompt string) (tea.Model, tea.Cmd) {
	mode, resumeID := m.session.RunOptions()
	run, err := cake.Start(cake.Options{
		Bin:          m.cfg.CakeBin,
		Cwd:          m.cfg.Cwd,
		Prompt:       prompt,
		Mode:         mode,
		ResumeID:     resumeID,
		Model:        m.cfg.Model,
		Profile:      m.cfg.Profile,
		Tools:        m.cfg.Tools,
		NoTools:      m.cfg.NoTools,
		AddDirs:      m.cfg.AddDirs,
		ToolboxDirs:  m.cfg.ToolboxDirs,
		Sandbox:      m.cfg.Sandbox,
		NoSkills:     m.cfg.NoSkills,
		Skills:       m.cfg.Skills,
		SystemPrompt: m.cfg.SystemPrompt,
		Fork:         m.cfg.Fork,
		ForkID:       m.cfg.ForkID,
		NoSession:    m.cfg.NoSession,
		DebugLog:     m.cfg.DebugLog,
		Registry:     m.cfg.Runs,
	})
	if err != nil {
		m.appendItem(ui.Item{Kind: ui.KindError, Text: err.Error()})
		return m, nil
	}

	m.run = run
	m.running = true
	m.sawComplete = false
	// A fork creates one fresh session boundary. Once the process has
	// started, future prompts must use the session pin rather than fork again.
	m.cfg.Fork = false
	if m.history.Add(prompt) && m.persistHistory(prompt) {
		m.appendItem(ui.Item{Kind: ui.KindWarning, Text: historyTooLargeWarning()})
	}
	m.appendItem(ui.Item{Kind: ui.KindUser, Text: prompt})
	m.input.Reset()
	m.clearSuggestion()
	m.layout()
	return m, tea.Batch(m.spin.Tick, waitForRun(run), m.titleCmd())
}

func (m *Model) applyEvent(ev cake.Event) {
	m.applyEventMode(ev, false)
}

func (m *Model) applyReplayEvent(ev cake.Event) {
	m.applyEventMode(ev, true)
}

func (m *Model) applyEventMode(ev cake.Event, replay bool) {
	switch e := ev.(type) {
	case cake.TaskStart:
		m.session.OnTaskStart(e)
		m.session.OnModel(e.ModelConfig, e.Model)
		m.appendItem(ui.Item{Kind: ui.KindTaskStart, Text: "task started · session " + ui.ShortID(e.SessionID)})

	case cake.SessionMeta:
		if replay {
			if e.SessionID != "" {
				m.session.SessionID = e.SessionID
			}
			// session_meta only appears in replay output; it carries the model
			// cake resolved for the resumed session.
			m.session.OnModel(e.ModelConfig, e.Model)
		}

	case cake.PromptContext:
		if replay {
			if e.SessionID != "" {
				m.session.SessionID = e.SessionID
			}
			if e.TaskID != "" {
				m.session.TaskID = e.TaskID
			}
		}

	case cake.SkillActivated:
		// Skill metadata is useful to the engine but has no dedicated timeline
		// representation. Keep it out of user output and debug-log a compact
		// diagnostic when configured.
		if e.Name != "" {
			m.logDebug("skill activated: " + e.Name)
		}

	case cake.Message:
		switch e.Role {
		case "assistant":
			if strings.TrimSpace(e.Content) != "" {
				// When cake emits function_call events before the assistant
				// message (the Chat Completions backend emits tool calls
				// before text), insert the assistant message before the
				// pending tool calls so the text appears before the tool
				// call in the timeline.
				if idx := m.firstPendingToolIdx(); idx >= 0 {
					m.insertItemAt(idx, ui.Item{Kind: ui.KindAssistant, Text: e.Content})
				} else {
					m.appendItem(ui.Item{Kind: ui.KindAssistant, Text: e.Content})
				}
			}
		case "user":
			if replay && strings.TrimSpace(e.Content) != "" {
				m.appendItem(ui.Item{Kind: ui.KindUser, Text: e.Content})
			}
			// The submitted prompt is already on the timeline for live runs.
		default:
			m.logDebug(fmt.Sprintf("%s message: %s", e.Role, e.Content))
		}

	case cake.Reasoning:
		// Reasoning content is deliberately not rendered: providers differ
		// widely in what they emit (summaries, raw text, or nothing at all),
		// so the timeline shows one coalesced "(thinking)" marker per burst
		// of reasoning events. Any other event type ends the burst.
		if !m.lastItemIsReasoning() {
			m.appendItem(ui.Item{Kind: ui.KindReasoning, Text: "(thinking)"})
		}

	case cake.FunctionCall:
		idx := m.appendItem(ui.Item{Kind: ui.KindTool, Tool: &ui.ToolBlock{
			Name:      e.Name,
			Arguments: e.Arguments,
		}})
		m.pendingCalls[e.CallID] = idx

	case cake.FunctionCallOutput:
		if idx, ok := m.pendingCalls[e.CallID]; ok && idx < len(m.items) && m.items[idx].Tool != nil {
			m.items[idx].Tool.Output = capRetainedToolOutput(e.Output)
			m.items[idx].Tool.Done = true
			delete(m.pendingCalls, e.CallID)
			m.rerenderItem(idx)
		} else {
			m.appendItem(ui.Item{Kind: ui.KindTool, Tool: &ui.ToolBlock{
				Name:      "(tool output)",
				Arguments: "{}",
				Output:    capRetainedToolOutput(e.Output),
				Done:      true,
			}})
		}

	case cake.HookEvent:
		if line, show := describeHook(e); show {
			m.appendItem(ui.Item{Kind: ui.KindHook, Text: line})
		} else {
			m.logDebug("hook " + line)
		}

	case cake.TurnUsage:
		// A live task and a replay both report the session's context here. The
		// status line reads the newest complete measurement; the record has no
		// timeline representation of its own.
		m.session.OnTurnUsage(e)

	case cake.Compaction:
		// Replay emits one compaction record at its physical position. It becomes
		// exactly one boundary marker: the retained tail is never expanded into
		// duplicate messages or tool calls. A replayed checkpoint also seeds the
		// status context with its own estimate, which any newer complete
		// turn_usage replaces. A live compaction stream record never seeds that
		// estimate; the live compactor result does it in finishCompaction.
		if replay {
			m.session.OnCompaction(e)
		}
		m.appendItem(ui.Item{
			Kind: ui.KindInfo,
			Text: compactionMarker(e.EstimatedInput.BeforeTokens, e.EstimatedInput.AfterTokens, e.RetainedCount),
		})

	case cake.TaskComplete:
		m.session.OnTaskComplete(e)
		m.session.OnModel(e.ModelConfig, e.Model)
		m.sawComplete = true
		if e.IsError {
			text := "task failed"
			if e.Error != "" {
				text += ": " + e.Error
			}
			m.appendItem(ui.Item{Kind: ui.KindError, Text: text})
		} else {
			m.appendItem(ui.Item{Kind: ui.KindComplete, Text: completionSummary(e)})
		}

	case cake.ReplayError:
		if replay {
			text := "session replay failed"
			if e.Kind != "" {
				text += " (" + e.Kind + ")"
			}
			if e.Error != "" {
				text += ": " + e.Error
			}
			m.replayWarn = true
			m.appendItem(ui.Item{Kind: ui.KindWarning, Text: text})
		}

	case cake.ParseError:
		if replay {
			m.replayWarn = true
		}
		m.appendItem(ui.Item{Kind: ui.KindWarning, Text: "malformed stream line: " + e.Line})

	case cake.Unknown:
		m.logDebug("unknown event type: " + e.Type)
	}
}

// logDebug records low-value diagnostics in the debug log; they never reach
// the timeline.
func (m *Model) logDebug(text string) {
	if m.cfg.DebugLog != nil {
		fmt.Fprintf(m.cfg.DebugLog, "repl: %s\n", text)
	}
}

func (m Model) finishReplay(res cake.Result) (tea.Model, tea.Cmd) {
	m.replayRun = nil
	m.replayPending = false
	if !m.hydrating {
		if m.exitAfter {
			return m.quit()
		}
		return m, nil
	}
	m.hydrating = false

	if !m.replayWarn {
		switch {
		case res.Canceled:
			m.appendItem(ui.Item{Kind: ui.KindWarning, Text: "session replay canceled"})
		case res.Err != nil:
			m.appendItem(ui.Item{Kind: ui.KindWarning, Text: "session replay failed: " + res.Err.Error()})
		case res.ExitCode != 0:
			text := fmt.Sprintf("session replay failed with exit code %d", res.ExitCode)
			if res.Stderr != "" {
				text += "\n" + ui.TruncateOutput(res.Stderr, m.cfg.OutputLimit)
			}
			m.appendItem(ui.Item{Kind: ui.KindWarning, Text: text})
		}
	}
	m.finishPendingTools()
	m.syncViewport()
	if m.exitAfter {
		return m.quit()
	}
	return m, nil
}

// finishPendingTools marks tool calls that never received an output as done,
// so a tool block does not keep showing "… running" once the stream has
// ended. Live runs and startup replay both reach this state: a task ends
// mid-call, or a replay transcript ends at a partial tail.
func (m *Model) finishPendingTools() {
	for callID, idx := range m.pendingCalls {
		if idx < len(m.items) && m.items[idx].Tool != nil {
			m.items[idx].Tool.Done = true
			m.items[idx].Tool.Output = "(no output — task ended)"
			m.rerenderItem(idx)
		}
		delete(m.pendingCalls, callID)
	}
}

func (m Model) finishRun(res cake.Result) (tea.Model, tea.Cmd) {
	m.running = false
	m.run = nil
	// The run has ended, so the title drops the working marker. Quitting paths
	// skip this: main writes the idle title once the program returns, which is
	// the only way to cover exits the model never observes.
	if m.newSessionPending {
		m.newSessionPending = false
		if m.exitAfter {
			return m.quit()
		}
		return m, m.titleCmd()
	}

	m.finishPendingTools()

	switch {
	case res.Canceled:
		m.appendItem(ui.Item{Kind: ui.KindWarning, Text: "canceled"})
	case res.Err != nil:
		m.appendItem(ui.Item{Kind: ui.KindError, Text: "cake failed: " + res.Err.Error()})
	case res.ExitCode != 0:
		text := fmt.Sprintf("cake exited with code %d", res.ExitCode)
		if res.Stderr != "" {
			text += "\n" + ui.TruncateOutput(res.Stderr, m.cfg.OutputLimit)
		}
		m.appendItem(ui.Item{Kind: ui.KindError, Text: text})
	case !m.sawComplete:
		m.appendItem(ui.Item{Kind: ui.KindWarning, Text: "cake exited before completing the task"})
	}

	// Whatever ended the run — cancellation, a nonzero exit, a wait error, or
	// a clean EOF before task_complete — a session id announced for this
	// invocation stays resumable. Pin it so the next prompt continues the same
	// session instead of orphaning the work. A completion already pinned
	// through OnTaskComplete, and pinning is idempotent.
	m.session.OnRunEnded()

	m.syncViewport()
	if m.exitAfter {
		return m.quit()
	}

	// Start both between-turn background calls from the same transition. The
	// suggester reads through cake's read-only replay stream, which tolerates
	// the compactor's concurrent append, so it does not wait for compaction to
	// start or finish. A result that arrives while compaction gates the composer
	// is held until finishCompaction applies it. See ADR 031.
	var cmds []tea.Cmd
	if m.suggestionTurnEligible(res) {
		next, cmd := m.maybeStartSuggestion()
		m = next
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	// Compact between turns, after the pin is settled and the completed turn's
	// measurement is known. The compactor re-checks the session and may decline.
	if m.shouldCompactAfter(res) {
		next, cmd := m.startCompaction()
		m = next
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	if len(cmds) == 0 {
		return m, m.titleCmd()
	}
	cmds = append(cmds, m.titleCmd())
	return m, tea.Batch(cmds...)
}

func (m Model) sessionInfo() string {
	orNone := func(s string) string {
		if s == "" {
			return "(none)"
		}
		return s
	}
	mode, resumeID := m.session.RunOptions()
	modeText := mode.String()
	if mode == cake.RunResume {
		modeText += " " + resumeID
	}
	lines := []string{
		"session:  " + orNone(m.session.SessionID),
		"task:     " + orNone(m.session.TaskID),
		"cwd:      " + m.cfg.Cwd,
		"next run: " + modeText,
	}
	if m.cfg.Model != "" {
		lines = append(lines, "model:    "+m.cfg.Model)
	}
	if m.cfg.Profile != "" {
		lines = append(lines, "profile:  "+m.cfg.Profile)
	}
	if m.cfg.Tools != "" {
		lines = append(lines, "tools:    "+m.cfg.Tools)
	}
	if c := m.session.LastComplete; c != nil {
		outcome := "success"
		if c.IsError {
			outcome = "error"
		}
		lines = append(lines, "last:     "+outcome+" · "+completionSummary(*c))
	}
	return strings.Join(lines, "\n")
}

// describeHook returns a one-line summary and whether it deserves timeline
// visibility. Successful hook noise stays hidden; denials, errors, and
// non-zero exits are shown.
func describeHook(e cake.HookEvent) (string, bool) {
	decision := e.ResolvedDecision
	if decision == "" {
		decision = e.Decision
	}
	var b strings.Builder
	b.WriteString("hook " + e.Event)
	if e.ToolName != "" {
		b.WriteString(" [" + e.ToolName + "]")
	}
	if decision != "" {
		b.WriteString(" → " + decision)
	}
	if e.ExitCode != nil && *e.ExitCode != 0 {
		fmt.Fprintf(&b, " (exit %d)", *e.ExitCode)
	}
	if stderr := strings.TrimSpace(e.Stderr); stderr != "" {
		lines := strings.SplitN(stderr, "\n", 2)
		b.WriteString(": " + lines[0])
	}

	// Cake's decision vocabulary is a closed set built in hooks.rs:
	// decision is none|deny|stop|error and resolved_decision adds allow.
	// Only the two benign values are hidden, so an unknown future value
	// shows up as timeline noise instead of a silently dropped denial.
	benign := decision == "" || decision == "allow" || decision == "none"
	failed := e.ExitCode != nil && *e.ExitCode != 0
	return b.String(), !benign || failed
}

// compactionMarker renders the one timeline marker for a context checkpoint:
// the estimated input reduction when cake reported one, plus how many records
// the checkpoint retained. The retained tail's content is never shown; the
// marker only stands where the checkpoint sits in the transcript. Replay and the
// live compactor result share this formatter, so both notices use one wording.
func compactionMarker(before, after, retained int) string {
	var b strings.Builder
	b.WriteString("context compacted")
	if before > 0 && after > 0 {
		fmt.Fprintf(&b, " %s -> %s", ui.CompactTokens(before), ui.CompactTokens(after))
	}
	if retained > 0 {
		fmt.Fprintf(&b, ", %d record%s retained", retained, plural(retained))
	}
	return b.String()
}

func completionSummary(e cake.TaskComplete) string {
	return fmt.Sprintf("done in %s · %d turn%s · %d tool call%s · %s tokens · session %s",
		humanDuration(e.DurationMS),
		e.TurnCount, plural(e.TurnCount),
		e.ToolCallCount, plural(e.ToolCallCount),
		fmt.Sprintf("%d in / %d out", e.Usage.InputTokens, e.Usage.OutputTokens),
		ui.ShortID(e.SessionID))
}

func humanDuration(ms int64) string {
	d := time.Duration(ms) * time.Millisecond
	switch {
	case d < time.Second:
		return fmt.Sprintf("%dms", ms)
	case d < time.Minute:
		return fmt.Sprintf("%.1fs", d.Seconds())
	case d < time.Hour:
		return fmt.Sprintf("%dm%02ds", int(d.Minutes()), int(d.Seconds())%60)
	default:
		h := int(d.Hours())
		m := int(d.Minutes()) % 60
		s := int(d.Seconds()) % 60
		return fmt.Sprintf("%dh%02dm%02ds", h, m, s)
	}
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
