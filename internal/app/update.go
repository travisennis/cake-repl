package app

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/travisennis/cake-repl/internal/cake"
	"github.com/travisennis/cake-repl/internal/ui"
)

// eventMsg delivers one decoded cake stream event.
type eventMsg struct{ ev cake.Event }

// runDoneMsg delivers the terminal state of a cake subprocess.
type runDoneMsg struct{ res cake.Result }

// waitForRun pulls the next event from a run, or its final result once the
// event stream is closed. It is re-issued after every event.
func waitForRun(run *cake.Run) tea.Cmd {
	return func() tea.Msg {
		if ev, ok := <-run.Events; ok {
			return eventMsg{ev}
		}
		return runDoneMsg{res: <-run.Result}
	}
}

// Update implements tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.layout()
		return m, nil

	case spinner.TickMsg:
		if !m.running {
			return m, nil
		}
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd

	case eventMsg:
		if !m.newSessionPending {
			m.applyEvent(msg.ev)
		}
		return m, waitForRun(m.run)

	case runDoneMsg:
		return m.finishRun(msg.res)

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
		return m.handleKey(msg)
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.CancelQuit):
		if m.running {
			m.run.Cancel()
			return m, nil
		}
		return m, tea.Quit

	case key.Matches(msg, m.keys.Submit):
		return m.submit()

	case key.Matches(msg, m.keys.NewSession):
		return m.startNewSession()

	case key.Matches(msg, m.keys.ClearInput):
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

	// Up/down recall prompt history only at the input's edge; inside a
	// multi-line input they keep moving the cursor.
	case key.Matches(msg, m.keys.HistoryPrev):
		if m.input.Line() == 0 {
			if text, ok := m.history.Prev(m.input.Value()); ok {
				m.input.SetValue(text)
				m.layout()
				return m, nil
			}
		}

	case key.Matches(msg, m.keys.HistoryNext):
		if m.input.Line() == m.input.LineCount()-1 {
			if text, ok := m.history.Next(); ok {
				m.input.SetValue(text)
				m.layout()
				return m, nil
			}
		}
	}

	// Any key that reaches here modifies the input — reset the completion cycle.
	m.completionPrefix = ""
	m.completionMatches = nil
	m.completionIdx = 0

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	m.layout() // input height tracks line count
	return m, cmd
}

// startNewSession creates an immediate visual and session-state boundary.
// When a run is active, its process is canceled and drained normally, but its
// remaining events are suppressed so they cannot enter or re-pin the new
// session.
func (m Model) startNewSession() (tea.Model, tea.Cmd) {
	if m.running {
		m.newSessionPending = true
		m.run.Cancel()
	}
	m.session.Reset()
	m.sawComplete = false
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

// handleTabComplete implements Tab key completion with cycling.
func (m Model) handleTabComplete() (tea.Model, tea.Cmd) {
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
	text := strings.TrimSpace(m.input.Value())
	if text == "" {
		return m, nil
	}

	if cmd, ok, err := ParseCommand(text); ok {
		if m.history.Add(text) {
			m.persistHistory(text)
		}
		m.input.Reset()
		m.layout()
		if err != nil {
			m.appendItem(ui.Item{Kind: ui.KindError, Text: err.Error()})
			return m, nil
		}
		return m.execCommand(cmd)
	}

	if m.newSessionPending {
		m.appendItem(ui.Item{Kind: ui.KindWarning, Text: "waiting for the canceled task to exit…"})
		return m, nil
	}

	if m.running {
		m.appendItem(ui.Item{Kind: ui.KindWarning, Text: "a task is already running — Ctrl+C cancels it"})
		return m, nil
	}
	return m.startRun(text)
}

func (m Model) execCommand(cmd Command) (tea.Model, tea.Cmd) {
	if m.running && (cmd.Kind == CmdNew || cmd.Kind == CmdContinue || cmd.Kind == CmdResume) {
		m.appendItem(ui.Item{Kind: ui.KindWarning, Text: "finish or cancel the running task first"})
		return m, nil
	}

	switch cmd.Kind {
	case CmdHelp:
		m.appendItem(ui.Item{Kind: ui.KindInfo, Text: HelpText})

	case CmdExit:
		if m.running {
			m.exitAfter = true
			m.run.Cancel()
			m.appendItem(ui.Item{Kind: ui.KindWarning, Text: "canceling task, then exiting…"})
			return m, nil
		}
		return m, tea.Quit

	case CmdNew:
		m.session.Reset()
		m.appendItem(ui.Item{Kind: ui.KindInfo, Text: "next prompt starts a fresh cake session"})

	case CmdContinue:
		m.session.UseContinue()
		m.appendItem(ui.Item{Kind: ui.KindInfo, Text: "next prompt continues cake's latest session"})

	case CmdResume:
		m.session.UseResume(cmd.Arg)
		m.appendItem(ui.Item{Kind: ui.KindInfo, Text: "next prompt resumes session " + cmd.Arg})

	case CmdSession:
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

// persistHistory appends one entry to the history file when configured.
// Write errors are silently ignored so a bad path never breaks the REPL.
//
// Each entry is stored as "\x02" + JSON-encoded prompt + "\n". The \x02
// prefix unambiguously marks JSONL records so loadHistory can distinguish
// them from legacy plain-text lines. Appending via O_APPEND is safe for
// concurrent writers.
func (m Model) persistHistory(text string) {
	if m.cfg.HistoryFile == "" {
		return
	}
	f, err := os.OpenFile(m.cfg.HistoryFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close() //nolint:errcheck // best-effort on close
	_, _ = f.WriteString(encodeHistoryEntry(text))
}

func (m Model) startRun(prompt string) (tea.Model, tea.Cmd) {
	mode, resumeID := m.session.RunOptions()
	run, err := cake.Start(cake.Options{
		Bin:      m.cfg.CakeBin,
		Cwd:      m.cfg.Cwd,
		Prompt:   prompt,
		Mode:     mode,
		ResumeID: resumeID,
		Model:    m.cfg.Model,
		Profile:  m.cfg.Profile,
		DebugLog: m.cfg.DebugLog,
	})
	if err != nil {
		m.appendItem(ui.Item{Kind: ui.KindError, Text: err.Error()})
		return m, nil
	}

	m.run = run
	m.running = true
	m.sawComplete = false
	if m.history.Add(prompt) {
		m.persistHistory(prompt)
	}
	m.appendItem(ui.Item{Kind: ui.KindUser, Text: prompt})
	m.input.Reset()
	m.layout()
	return m, tea.Batch(m.spin.Tick, waitForRun(run))
}

func (m *Model) applyEvent(ev cake.Event) {
	switch e := ev.(type) {
	case cake.TaskStart:
		m.session.OnTaskStart(e)
		m.appendItem(ui.Item{Kind: ui.KindTaskStart, Text: "task started · session " + ui.ShortID(e.SessionID)})

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
			// The submitted prompt is already on the timeline.
		default:
			m.logDebug(fmt.Sprintf("%s message: %s", e.Role, e.Content))
		}

	case cake.Reasoning:
		text := strings.TrimSpace(strings.Join(e.Summary, "\n"))
		if text != "" {
			m.appendItem(ui.Item{Kind: ui.KindReasoning, Text: text})
		}

	case cake.FunctionCall:
		idx := m.appendItem(ui.Item{Kind: ui.KindTool, Tool: &ui.ToolBlock{
			Name:      e.Name,
			Arguments: e.Arguments,
		}})
		m.pendingCalls[e.CallID] = idx

	case cake.FunctionCallOutput:
		if idx, ok := m.pendingCalls[e.CallID]; ok && idx < len(m.items) && m.items[idx].Tool != nil {
			m.items[idx].Tool.Output = e.Output
			m.items[idx].Tool.Done = true
			delete(m.pendingCalls, e.CallID)
			m.rerenderItem(idx)
		} else {
			m.appendItem(ui.Item{Kind: ui.KindTool, Tool: &ui.ToolBlock{
				Name:      "(tool output)",
				Arguments: "{}",
				Output:    e.Output,
				Done:      true,
			}})
		}

	case cake.HookEvent:
		if line, show := describeHook(e); show {
			m.appendItem(ui.Item{Kind: ui.KindHook, Text: line})
		} else {
			m.logDebug("hook " + line)
		}

	case cake.TaskComplete:
		m.session.OnTaskComplete(e)
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

	case cake.ParseError:
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

func (m Model) finishRun(res cake.Result) (tea.Model, tea.Cmd) {
	m.running = false
	m.run = nil
	if m.newSessionPending {
		m.newSessionPending = false
		if m.exitAfter {
			return m, tea.Quit
		}
		return m, nil
	}

	// Any tool call still waiting for output will never get one.
	for callID, idx := range m.pendingCalls {
		if idx < len(m.items) && m.items[idx].Tool != nil {
			m.items[idx].Tool.Done = true
			m.items[idx].Tool.Output = "(no output — task ended)"
			m.rerenderItem(idx)
		}
		delete(m.pendingCalls, callID)
	}

	switch {
	case res.Canceled:
		m.session.OnCancel()
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

	if m.exitAfter {
		return m, tea.Quit
	}
	return m, nil
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
