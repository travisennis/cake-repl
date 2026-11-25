package app

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/atotto/clipboard"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/travisennis/cake-repl/internal/cake"
	"github.com/travisennis/cake-repl/internal/ui"
)

const maxEventsPerBatch = 10

const maxRetainedToolOutputBytes = 1 << 20

func capRetainedToolOutput(output string) string {
	return ui.CapOwned(output, maxRetainedToolOutputBytes)
}

type eventsMsg struct {
	key operationKey
	evs []cake.Event
}

type runDoneMsg struct {
	key operationKey
	res cake.Result
}

type clipboardResultMsg struct {
	sessionID bool
	chars     int
	err       error
}

func waitForRun(run *cake.Run, key operationKey) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-run.Events
		if !ok {
			return runDoneMsg{key: key, res: <-run.Result}
		}
		evs := []cake.Event{ev}
		for len(evs) < maxEventsPerBatch {
			select {
			case ev, ok := <-run.Events:
				if !ok {
					return eventsMsg{key: key, evs: evs}
				}
				evs = append(evs, ev)
			default:
				return eventsMsg{key: key, evs: evs}
			}
		}
		return eventsMsg{key: key, evs: evs}
	}
}

type sessionsLoadedMsg struct {
	req      uint64
	sessions []cake.SessionSummary
	err      error
}

func (m Model) startSessionListLoad() (Model, tea.Cmd) {
	m.cancelSessionList()
	m.sessionListSeq++
	m.sessionListReq = m.sessionListSeq
	ctx, cancel := context.WithCancel(context.Background())
	m.sessionListCancel = cancel
	if m.sessionListDone == nil {
		m.sessionListDone = map[uint64]chan struct{}{}
	}
	done := make(chan struct{})
	req := m.sessionListReq
	m.sessionListDone[req] = done
	m.trackChild(fmt.Sprintf("session listing %d", req), cancel, done)
	cfg := m.cfg
	return m, func() tea.Msg {
		sessions, err := cake.ListSessions(ctx, cake.SessionsOptions{
			Bin:      cfg.CakeBin,
			Cwd:      cfg.Cwd,
			DebugLog: cfg.DebugLog,
		})
		close(done)
		return sessionsLoadedMsg{req: req, sessions: sessions, err: err}
	}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.layout()
		m.syncViewport()
		return m, nil

	case trustStatusMsg:
		m.handleTrustStatus(msg)
		m.syncViewport()
		return m, nil

	case spinner.TickMsg:
		if !m.runBusy() && !m.compactionBusy() && !m.suggestionInFlight() {
			return m, nil
		}
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		now := msg.Time
		if now.IsZero() {
			now = time.Now()
		}
		if m.compactionBusy() {
			m.refreshCompactionElapsed(now)
		}
		if m.suggestionInFlight() {
			m.refreshSuggestionElapsed(now)
		}
		return m, cmd

	case compactionDoneMsg:
		return m.finishCompaction(msg)

	case suggestionDoneMsg:
		next := m.finishSuggestion(msg)
		if next.exiting() {
			return next.maybeQuit()
		}
		return next, nil

	case replayStartedMsg:
		return m.finishReplayStart(msg)

	case replayEventsMsg:
		return m.handleReplayEvents(msg)

	case replayDoneMsg:
		return m.finishReplay(msg)

	case eventsMsg:
		if m.run == nil || msg.key != m.runOp.key {
			return m, nil
		}
		if msg.key.generation == m.session.Generation() {
			for _, ev := range msg.evs {
				m.applyEvent(ev)
			}
		}
		m.syncViewport()
		return m, waitForRun(m.run, msg.key)

	case runDoneMsg:
		return m.finishRunMsg(msg)

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
		if m.sessionListDone != nil {
			delete(m.sessionListDone, msg.req)
		}
		if m.exiting() {
			return m.maybeQuit()
		}
		if msg.req != m.sessionListReq {
			return m, nil
		}
		m.sessionListCancel = nil
		if msg.err != nil {
			m.browser.failed(ui.TrimForDisplay(msg.err.Error(), m.cfg.OutputLimit))
			m.resetSessionList()
			return m, nil
		}
		now := time.Now()
		items := make([]list.Item, 0, len(msg.sessions))
		for _, s := range msg.sessions {
			items = append(items, newSessionItem(s, now))
		}
		m.sessionList.SetItems(items)
		m.sessionList.ResetSelected()
		m.browser.loaded(len(items))
		return m, nil

	case tea.MouseMsg:
		if msg.Action == tea.MouseActionPress &&
			(msg.Button == tea.MouseButtonWheelUp || msg.Button == tea.MouseButtonWheelDown) {
			var cmd tea.Cmd
			m.viewport, cmd = m.viewport.Update(msg)
			return m, cmd
		}

	case tea.KeyMsg:
		if msg.Paste {
			model := m.handlePaste(string(msg.Runes))
			model.syncViewport()
			return model, nil
		}
		mm, cmd := m.handleKey(msg)
		m = mm.(Model)
		m.syncViewport()
		return m, cmd
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m Model) quit() (Model, tea.Cmd) {
	if !m.exiting() {
		m.exitPhase = phaseDraining
		if m.suggestion.doneChannel() != nil {
			m.appendItem(ui.Item{Kind: ui.KindWarning, Text: "canceling prompt suggestion, then exiting…"})
		}
	}
	m.cancelRun()
	m.cancelReplay()
	m.cancelCompaction()
	m.cancelSuggestion()
	m.cancelSessionList()
	m.syncViewport()
	return m.afterPresentationChange()
}

func (m Model) afterPresentationChange() (Model, tea.Cmd) {
	next, cmd := m.maybeQuit()
	if cmd != nil {
		return next, cmd
	}
	return next, next.titleCmd()
}

func (m Model) maybeQuit() (Model, tea.Cmd) {
	if !m.exiting() {
		return m, nil
	}
	if m.runBusy() || m.replayBusy() || m.compactionBusy() ||
		m.suggestionBusy() || len(m.sessionListDone) > 0 {
		return m, nil
	}
	return m, tea.Quit
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.exiting() {
		return m, nil
	}
	if m.browser.open() {
		return m.handleBrowserKey(msg)
	}
	switch {
	case key.Matches(msg, m.keys.CancelQuit):
		if m.runBusy() {
			m.cancelRun()
			return m, m.titleCmd()
		}
		if m.compactionBusy() {
			m.cancelCompaction()
			return m, m.titleCmd()
		}
		return m.quit()

	case key.Matches(msg, m.keys.Submit):
		return m.submit()

	case key.Matches(msg, m.keys.NewSession):
		return m.startNewSession()

	case key.Matches(msg, m.keys.ClearInput):
		m.input.Reset()
		m.suggestion.filledText = ""
		m.layout()
		m.hideSuggestion()
		return m, nil

	case key.Matches(msg, m.keys.PageUp), key.Matches(msg, m.keys.PageDown):
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		return m, cmd

	case key.Matches(msg, m.keys.TabComplete):
		return m.handleTabComplete()

	case key.Matches(msg, m.keys.ToggleToolOutput):
		return m.toggleToolOutput()

	case key.Matches(msg, m.keys.CopyLastAssistant):
		return m.copyLastAssistant()

	case key.Matches(msg, m.keys.HistoryPrev):
		if m.input.Line() == 0 {
			if text, ok := m.history.Prev(m.input.Value()); ok {
				m.hideSuggestionForHistory()
				m.setComposerValue(text)
				m.layout()
				return m, nil
			}
		}

	case key.Matches(msg, m.keys.HistoryNext):
		if m.input.Line() == m.input.LineCount()-1 {
			if text, ok := m.history.Next(); ok {
				m.hideSuggestionForHistory()
				m.setComposerValue(text)
				m.layout()
				return m, nil
			}
		}
	}

	m.hideSuggestion()
	m.completionPrefix = ""
	m.completionMatches = nil
	m.completionIdx = 0

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	if m.suggestion.filledText != "" && !m.filledSuggestion() {
		m.suggestion.filledText = ""
	}
	m.layout()
	return m, cmd
}

func (m Model) handlePaste(text string) Model {
	if m.exiting() || m.browser.open() || text == "" {
		return m
	}
	m.hideSuggestion()
	m.completionPrefix = ""
	m.completionMatches = nil
	m.completionIdx = 0
	bounded, truncated := boundComposerText(text, m.input.LineCount())
	if bounded != "" {
		m.input.InsertString(bounded)
	}
	if truncated {
		m.appendItem(ui.Item{Kind: ui.KindWarning, Text: composerTruncationWarning()})
	}
	m.layout()
	return m
}

func (m *Model) setComposerValue(text string) bool {
	bounded, truncated := boundComposerText(text, 1)
	m.input.SetValue(bounded)
	return truncated
}

func boundComposerText(text string, currentLines int) (string, bool) {
	truncated := false
	if len(text) > maxComposerBytes {
		cut := maxComposerBytes
		for cut > 0 && !utf8.RuneStart(text[cut]) {
			cut--
		}
		text = text[:cut]
		truncated = true
	}
	allowed := maxComposerLines - currentLines + 1
	if allowed < 1 {
		allowed = 1
	}
	if strings.Count(text, "\n")+1 > allowed {
		lines := strings.Split(text, "\n")
		text = strings.Join(lines[:allowed], "\n")
		truncated = true
	}
	return text, truncated
}

func composerTruncationWarning() string {
	return fmt.Sprintf("pasted text truncated at %d lines or %d MiB", maxComposerLines, maxComposerBytes>>20)
}

func (m Model) handleBrowserKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "q", "ctrl+c":
		m.closeBrowser()
		return m, m.titleCmd()
	case "enter":
		if !m.browserSelectable() {
			return m, nil
		}
		if it, ok := m.sessionList.SelectedItem().(sessionItem); ok {
			m.closeBrowser()
			next, _ := m.resumeSession(it.summary.SessionID)
			model := next.(Model)
			return model, model.titleCmd()
		}
		return m, nil
	case "up", "down", "pgup", "pgdown", "home", "end":
		var cmd tea.Cmd
		m.sessionList, cmd = m.sessionList.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m Model) startNewSession() (tea.Model, tea.Cmd) {
	m.clearSuggestion()
	busy := m.runBusy() || m.replayBusy() || m.compactionBusy()
	m.session.Reset()
	if m.runBusy() {
		m.cancelRun()
	}
	if m.compactionBusy() {
		m.cancelCompaction()
	}
	if m.replayBusy() {
		m.cancelReplay()
	}
	m.sawComplete = false
	m.runOutcome = outcomeNone
	m.runGeneration = 0
	m.timeline.clear()
	m.pendingCalls = map[string]*ui.ToolBlock{}
	m.native.reset()
	m.appendItem(ui.Item{Kind: ui.KindInfo, Text: "New session"})
	if busy {
		return m, m.titleCmd()
	}
	return m, nil
}

func (m Model) toggleToolOutput() (tea.Model, tea.Cmd) {
	m.toolOutputMode = m.toolOutputMode.Cycle()
	m.rerenderToolItems()
	return m, nil
}

func (m Model) clipboardCmd(text string, sessionID bool) tea.Cmd {
	chars := utf8.RuneCountInString(text)
	return func() tea.Msg {
		return clipboardResultMsg{sessionID: sessionID, chars: chars, err: clipboard.WriteAll(text)}
	}
}

func (m Model) copyLastAssistant() (tea.Model, tea.Cmd) {
	text, ok := m.lastAssistantMarkdown()
	if !ok {
		m.appendItem(ui.Item{Kind: ui.KindWarning, Text: "no assistant response to copy"})
		return m, nil
	}
	return m, m.clipboardCmd(text, false)
}

func (m Model) copySessionID() (tea.Model, tea.Cmd) {
	id, _ := m.SessionData()
	if id == "" {
		m.appendItem(ui.Item{Kind: ui.KindWarning, Text: "no session id to copy"})
		return m, nil
	}
	return m, m.clipboardCmd(id, true)
}

func (m Model) handleTabComplete() (next tea.Model, cmd tea.Cmd) {
	before := m.input.Value()
	filled := m.suggestion.filledText
	defer func() {
		if model, ok := next.(Model); ok {
			if model.input.Value() == before && filled != "" {
				model.suggestion.filledText = filled
			}
			next = model
		}
	}()
	m.clearSuggestion()
	input := m.input.Value()

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

	if m.completionPrefix == "" {
		matches, ok := completeSlash(input, known)
		if !ok {
			return m, nil
		}
		if len(matches) == 1 {
			m.input.SetValue(matches[0])
			m.layout()
			return m, nil
		}
		m.completionPrefix = input
		m.completionMatches = matches
		m.completionIdx = 0
		m.input.SetValue(matches[0])
		m.layout()
		return m, nil
	}

	m.completionIdx++
	if m.completionIdx >= len(m.completionMatches) {
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
	if m.exiting() {
		return m, nil
	}
	text := strings.TrimSpace(m.input.Value())
	if text == "" && m.suggestionActive() {
		m.fillSuggestion()
		return m, nil
	}
	if text == "" {
		return m, nil
	}

	fromSuggestion := m.filledSuggestion()
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

	if gate := m.admitPrompt(); !gate.allowed {
		if gate.text != "" {
			m.appendItem(ui.Item{Kind: ui.KindWarning, Text: gate.text})
		}
		return m, nil
	}
	return m.startRun(text)
}

func needsIdle(kind CommandKind) bool {
	return kind == CmdNew || kind == CmdResume || kind == CmdSessions ||
		kind == CmdModel || kind == CmdProfile
}

func (m Model) execCommand(cmd Command) (tea.Model, tea.Cmd) {
	if m.exiting() {
		return m, nil
	}
	if needsIdle(cmd.Kind) {
		if gate := m.admitRetarget(); !gate.allowed {
			if gate.text != "" {
				m.appendItem(ui.Item{Kind: ui.KindWarning, Text: gate.text})
			}
			return m, nil
		}
	}

	switch cmd.Kind {
	case CmdHelp:
		m.appendItem(ui.Item{Kind: ui.KindInfo, Text: HelpText})

	case CmdExit:
		switch {
		case m.runBusy():
			m.appendItem(ui.Item{Kind: ui.KindWarning, Text: "canceling task, then exiting…"})
		case m.compactionBusy():
			m.appendItem(ui.Item{Kind: ui.KindWarning, Text: "canceling context compaction, then exiting…"})
		case m.replayBusy():
			m.appendItem(ui.Item{Kind: ui.KindWarning, Text: "canceling session history load, then exiting…"})
		}
		return m.quit()

	case CmdNew:
		m.clearSuggestion()
		m.session.Reset()
		m.native.reset()
		m.appendItem(ui.Item{Kind: ui.KindInfo, Text: "next prompt starts a fresh cake session"})

	case CmdResume:
		return m.resumeSession(cmd.Arg)

	case CmdSessions:
		next, cmd := m.openSessionBrowser()
		return next, tea.Batch(cmd, next.titleCmd())

	case CmdSession:
		if cmd.Arg == "copy" {
			return m.copySessionID()
		}
		m.appendItem(ui.Item{Kind: ui.KindInfo, Text: m.sessionInfo()})

	case CmdModel:
		switch cmd.Arg {
		case "":
			m.appendItem(ui.Item{Kind: ui.KindInfo, Text: m.modelSelectionStatus()})
		case "default":
			m.selectModel("")
			m.appendItem(ui.Item{Kind: ui.KindInfo, Text: m.selectionClearedText("model")})
		default:
			m.selectModel(cmd.Arg)
			m.appendItem(ui.Item{Kind: ui.KindInfo, Text: "next prompt model: " + cmd.Arg})
		}

	case CmdProfile:
		switch cmd.Arg {
		case "":
			m.appendItem(ui.Item{Kind: ui.KindInfo, Text: m.profileSelectionStatus()})
		case "default":
			m.selectProfile("")
			m.appendItem(ui.Item{Kind: ui.KindInfo, Text: m.selectionClearedText("profile")})
		default:
			m.selectProfile(cmd.Arg)
			m.appendItem(ui.Item{Kind: ui.KindInfo, Text: "next prompt profile: " + cmd.Arg})
		}

	case CmdClear:
		m.timeline.clear()
		m.pendingCalls = map[string]*ui.ToolBlock{}
		m.native.reset()
	}
	return m, nil
}

func (m *Model) selectModel(model string) {
	m.turn.Model = model
	m.turn.ModelExplicit = model != ""
	if mode, _ := m.session.RunOptions(); mode != cake.RunResume {
		m.session.Model = ""
	}
	m.session.Context = nil
}

func (m *Model) selectProfile(profile string) {
	m.turn.Profile = profile
	m.turn.ProfileExplicit = profile != ""
	if mode, _ := m.session.RunOptions(); mode != cake.RunResume {
		m.session.Model = ""
		m.session.Profile = ""
		m.session.ProfileKnown = false
	}
	m.session.Context = nil
}

func (m Model) modelSelectionStatus() string {
	if model := m.displayModel(); model != "" {
		return "model: " + model
	}
	return "model: (cake default)"
}

func (m Model) profileSelectionStatus() string {
	if profile := m.effectiveProfile(); profile != "" {
		return "profile: " + profile
	}
	return "profile: (cake default)"
}

func (m Model) selectionClearedText(kind string) string {
	if mode, _ := m.session.RunOptions(); mode == cake.RunResume {
		return "next prompt " + kind + ": (stored session value)"
	}
	return "next prompt " + kind + ": (cake default)"
}

func (m Model) resumeSession(id string) (tea.Model, tea.Cmd) {
	m.clearSuggestion()
	m.session.UseResume(id)
	m.appendItem(ui.Item{Kind: ui.KindInfo, Text: "next prompt resumes session " + id})
	return m, nil
}

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

func (m Model) runOptions(prompt string) cake.Options {
	mode, resumeID := m.session.RunOptions()
	return m.runOptionsFor(mode, resumeID, prompt)
}

func (m Model) runOptionsFor(mode cake.RunMode, resumeID, prompt string) cake.Options {
	restore := mode == cake.RunResume
	opts := cake.Options{
		Bin:       m.cfg.CakeBin,
		Cwd:       m.cfg.Cwd,
		Prompt:    prompt,
		Mode:      mode,
		ResumeID:  resumeID,
		Fork:      m.cfg.Fork,
		ForkID:    m.cfg.ForkID,
		NoSession: m.cfg.NoSession,
		DebugLog:  m.cfg.DebugLog,
		Registry:  m.cfg.Runs,
	}
	if restore && !m.session.RestoreKnown && m.session.Creation != nil {
		m.session.Creation.apply(&opts)
	}
	if !restore || m.turn.ModelExplicit {
		opts.Model = m.turn.Model
	}
	if !restore || m.turn.ProfileExplicit {
		opts.Profile = m.turn.Profile
	}
	if !restore || m.cfg.Explicit["add-dir"] {
		opts.AddDirs = m.cfg.AddDirs
	}
	if !restore || m.cfg.Explicit["toolbox"] {
		opts.ToolboxDirs = m.cfg.ToolboxDirs
	}
	if !restore || m.cfg.Explicit["sandbox"] {
		opts.Sandbox = m.cfg.Sandbox
	}
	if !restore || m.cfg.Explicit["tools"] {
		opts.Tools = m.cfg.Tools
	}
	if !restore || m.cfg.Explicit["no-tools"] {
		opts.NoTools = m.cfg.NoTools
	}
	if !restore || m.cfg.Explicit["no-skills"] {
		opts.NoSkills = m.cfg.NoSkills
	}
	if !restore || m.cfg.Explicit["skills"] {
		opts.Skills = m.cfg.Skills
	}
	if !restore || m.cfg.Explicit["system-prompt"] {
		opts.SystemPrompt = m.cfg.SystemPrompt
	}
	return opts
}

func (m Model) startRun(prompt string) (tea.Model, tea.Cmd) {
	if m.exiting() || m.runBusy() || m.compactionBusy() {
		return m, nil
	}
	return m.startRunCmd(prompt, m.runOptions(prompt), true)
}

func (m Model) startRunCmd(prompt string, opts cake.Options, record bool) (tea.Model, tea.Cmd) {
	m.sawComplete = false
	m.runOutcome = outcomeNone
	m.runGeneration = 0
	m.suggestion.hydrated = false
	m.runPrompt = prompt
	run, err := cake.Start(opts)
	if err != nil {
		m.appendItem(ui.Item{Kind: ui.KindError, Text: err.Error()})
		return m, nil
	}
	if opts.Mode == cake.RunFresh {
		m.session.Creation = controlsFromOptions(opts)
	}
	m.trackChild("run", run.Cancel, run.Done)

	resumeID := ""
	if opts.Mode == cake.RunResume {
		resumeID = opts.ResumeID
	}
	m.runSeq++
	key := operationKey{generation: m.session.Generation(), request: m.runSeq, sessionID: resumeID}
	m.runOp.begin(key, run.Cancel, run.Done)
	m.run = run
	m.cfg.Fork = false
	if record {
		if m.history.Add(prompt) && m.persistHistory(prompt) {
			m.appendItem(ui.Item{Kind: ui.KindWarning, Text: historyTooLargeWarning()})
		}
		m.appendItem(ui.Item{Kind: ui.KindUser, Text: prompt})
		m.input.Reset()
	}
	m.clearSuggestion()
	m.layout()
	return m, tea.Batch(m.spin.Tick, waitForRun(run, key), m.titleCmd())
}

func (m Model) forkRetryOptions(sourceID string) cake.Options {
	opts := m.runOptionsFor(cake.RunFresh, "", m.runPrompt)
	opts.Fork = true
	opts.ForkID = sourceID
	opts.NoSession = false
	if !m.turn.ModelExplicit {
		opts.Model = ""
	}
	if m.turn.ProfileExplicit || m.session.ProfileKnown {
		opts.Profile = m.effectiveProfile()
	}
	return opts
}

func (m Model) retryFork(sourceID string) (tea.Model, tea.Cmd) {
	m.appendItem(ui.Item{
		Kind: ui.KindWarning,
		Text: "session context changed — retrying as a fork of " + ui.ShortID(sourceID),
	})
	m.syncViewport()
	return m.startRunCmd(m.runPrompt, m.forkRetryOptions(sourceID), false)
}

func (m *Model) applyEvent(ev cake.Event) {
	m.applyEventMode(ev, false)
}

func (m *Model) applyReplayEvent(ev cake.Event) bool {
	return m.applyEventMode(ev, true)
}

func (m *Model) applyEventMode(ev cake.Event, replay bool) bool {
	cwdRestored := false
	switch e := ev.(type) {
	case cake.TaskStart:
		m.session.OnTaskStart(e)
		m.session.OnModel(e.ModelConfig, e.Model)
		m.appendItem(ui.Item{Kind: ui.KindTaskStart, Text: "task started · session " + ui.ShortID(e.SessionID)})
		if replay {
			m.sawComplete = false
		}

	case cake.SessionMeta:
		if replay {
			if e.SessionID != "" {
				m.session.SessionID = e.SessionID
			}
			m.session.OnModel(e.ModelConfig, e.Model)
			m.session.OnStoredRunConfig(e.RunConfig, e.Profile)
			if m.cfg.RestoreCwd && e.Cwd != "" {
				if info, err := os.Stat(e.Cwd); err == nil && info.IsDir() {
					m.cfg.Cwd = e.Cwd
					m.applyRestoredProjectHelpers(e.Cwd)
					m.cfg.RestoreCwd = false
					cwdRestored = true
				} else {
					m.replay.warned = true
					m.appendItem(ui.Item{Kind: ui.KindWarning, Text: "session working directory is unavailable: " + e.Cwd})
				}
			}
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
		if e.Name != "" {
			m.logDebug("skill activated: " + e.Name)
		}

	case cake.Message:
		switch e.Role {
		case "assistant":
			if strings.TrimSpace(e.Content) != "" {
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
		default:
			m.logDebug(fmt.Sprintf("%s message: %s", e.Role, e.Content))
		}

	case cake.Reasoning:
		if !m.lastItemIsReasoning() {
			m.appendItem(ui.Item{Kind: ui.KindReasoning, Text: "(thinking)"})
		}

	case cake.FunctionCall:
		tool := &ui.ToolBlock{
			Name:      e.Name,
			Arguments: e.Arguments,
		}
		m.appendItem(ui.Item{Kind: ui.KindTool, Tool: tool})
		m.pendingCalls[e.CallID] = tool

	case cake.FunctionCallOutput:
		if tool, idx, ok := m.pendingTool(e.CallID); ok {
			tool.Output = capRetainedToolOutput(e.Output)
			tool.Done = true
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
		m.session.OnTurnUsage(e)

	case cake.Compaction:
		if replay {
			m.session.OnCompaction(e)
			m.appendItem(ui.Item{
				Kind: ui.KindInfo,
				Text: compactionMarker(e.EstimatedInput.BeforeTokens, e.EstimatedInput.AfterTokens, e.RetainedCount),
			})
		} else {
			m.nativeCompactionCheckpoint(e)
		}

	case cake.CompactionStart:
		if replay {
			m.logDebug("compaction started: " + e.Trigger)
		} else {
			m.nativeCompactionStart(e)
		}

	case cake.CompactionEnd:
		if replay {
			m.nativeCompactionEndReplay(e)
		} else {
			m.nativeCompactionEnd(e)
		}

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
			m.replay.warned = true
			m.appendItem(ui.Item{Kind: ui.KindWarning, Text: text})
		}

	case cake.ParseError:
		if replay {
			m.replay.warned = true
		}
		m.appendItem(ui.Item{Kind: ui.KindWarning, Text: "malformed stream line: " + e.Line})

	case cake.Unknown:
		m.logDebug("unknown event type: " + e.Type)
	}
	return cwdRestored
}

func (m *Model) applyRestoredProjectHelpers(cwd string) {
	if m.cfg.ReloadProjectHelpers == nil {
		return
	}
	helpers, err := m.cfg.ReloadProjectHelpers(cwd)
	if err != nil {
		m.cfg.Compaction.Enabled = false
		m.cfg.Suggestions.Enabled = false
		m.appendItem(ui.Item{Kind: ui.KindWarning, Text: "project config: " + err.Error()})
		return
	}
	if m.cfg.NoSession {
		return
	}
	m.cfg.Compaction.Enabled = helpers.CompactionEnabled
	m.cfg.Suggestions.Enabled = helpers.SuggestionsEnabled
}

func (m *Model) logDebug(text string) {
	if m.cfg.DebugLog != nil {
		fmt.Fprintf(m.cfg.DebugLog, "repl: %s\n", text)
	}
}

func (m *Model) finishPendingTools() {
	for callID := range m.pendingCalls {
		tool, idx, ok := m.pendingTool(callID)
		if ok {
			tool.Done = true
			tool.Output = "(no output — task ended)"
			m.rerenderItem(idx)
		}
		delete(m.pendingCalls, callID)
	}
}

func (m Model) finishRunMsg(msg runDoneMsg) (tea.Model, tea.Cmd) {
	return m.finishRunKey(msg.key, msg.res)
}

func (m Model) finishRunKey(key operationKey, res cake.Result) (tea.Model, tea.Cmd) {
	if !m.runOp.settle(key) {
		return m.maybeQuit()
	}
	m.run = nil

	if key.generation != m.session.Generation() {
		m.sawComplete = false
		m.runOutcome = outcomeNone
		m.runGeneration = 0
		if m.exiting() {
			return m.maybeQuit()
		}
		return m, m.titleCmd()
	}

	m.finishPendingTools()
	if m.native.pending {
		text := "context compaction incomplete"
		if res.Canceled {
			text = "context compaction canceled"
		}
		m.settleNativeCompaction(text)
	}

	result := outcomeSuccess
	switch {
	case res.Canceled:
		result = outcomeCanceled
		m.appendItem(ui.Item{Kind: ui.KindWarning, Text: "canceled"})
	case res.Err != nil:
		result = outcomeFailure
		m.appendItem(ui.Item{Kind: ui.KindError, Text: "cake failed: " + res.Err.Error()})
	case res.ContextMismatch() && key.sessionID != "" && !m.cfg.NoSession:
		m.session.OnRunEnded()
		m.syncViewport()
		if m.exiting() {
			return m.maybeQuit()
		}
		return m.retryFork(key.sessionID)
	case res.ExitCode != 0:
		result = outcomeFailure
		text := fmt.Sprintf("cake exited with code %d", res.ExitCode)
		if res.Stderr != "" {
			text += "\n" + ui.TrimForDisplay(res.Stderr, m.cfg.OutputLimit)
		}
		m.appendItem(ui.Item{Kind: ui.KindError, Text: text})
	case !m.sawComplete:
		result = outcomeFailure
		m.appendItem(ui.Item{Kind: ui.KindWarning, Text: "cake exited before completing the task"})
	case m.session.LastComplete == nil || m.session.LastComplete.IsError:
		result = outcomeFailure
	}
	m.sawComplete = false
	m.runOutcome = result
	m.runGeneration = key.generation

	m.session.OnRunEnded()

	m.syncViewport()
	if m.exiting() {
		return m.maybeQuit()
	}

	var cmds []tea.Cmd
	if m.completedTurn() {
		next, cmd := m.maybeStartSuggestion()
		m = next
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	if m.shouldCompactAfter() {
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
	if model := m.displayModel(); model != "" {
		lines = append(lines, "model:    "+model)
	}
	if m.turn.Profile != "" {
		lines = append(lines, "profile:  "+m.turn.Profile)
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

	benign := decision == "" || decision == "allow" || decision == "none"
	failed := e.ExitCode != nil && *e.ExitCode != 0
	return b.String(), !benign || failed
}

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
