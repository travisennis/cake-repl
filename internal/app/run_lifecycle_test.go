package app

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/travisennis/cake-repl/internal/cake"
	"github.com/travisennis/cake-repl/internal/compact"
	"github.com/travisennis/cake-repl/internal/ui"
)

// beginTestRun puts the model into the active phase without launching a child.
// Tests that need cancellation or reaping supply their own handles through
// beginTestRunHandle.
func beginTestRun(m *Model) operationKey {
	m.runSeq++
	key := operationKey{generation: m.session.Generation(), request: m.runSeq}
	m.runOp.begin(key, nil, make(chan struct{}))
	m.run = &cake.Run{}
	return key
}

// beginTestRunHandle is beginTestRun with a real run handle. It wires the
// handle's Cancel and Done into the operation so shutdown and Ctrl+C tests use
// the same settlement path as production.
func beginTestRunHandle(m *Model, run *cake.Run) operationKey {
	m.runSeq++
	key := operationKey{generation: m.session.Generation(), request: m.runSeq}
	m.runOp.begin(key, run.Cancel, run.Done)
	m.run = run
	return key
}

// eventBatch builds a result for the current run request. Tests that exercise a
// stale batch pass their saved key explicitly instead.
func eventBatch(m Model, evs ...cake.Event) eventsMsg {
	return eventsMsg{key: m.runOp.key, evs: evs}
}

// markCompletedTurn records the result state a clean current-generation run
// leaves behind, for tests that exercise a post-turn decision directly.
func markCompletedTurn(m *Model) {
	m.sawComplete = true
	m.runOutcome = outcomeSuccess
	m.runGeneration = m.session.Generation()
}

func TestSessionGenerationAdvancesOnlyOnTargetChanges(t *testing.T) {
	s := newSessionState(cake.RunFresh, "")
	if got := s.Generation(); got != 1 {
		t.Fatalf("initial generation = %d, want 1", got)
	}

	s.OnTaskStart(cake.TaskStart{SessionID: "s-1", TaskID: "t-1"})
	s.OnTaskComplete(success("s-1"))
	s.OnRunEnded()
	if got := s.Generation(); got != 1 {
		t.Fatalf("announcing and ending a run advanced the generation: %d", got)
	}

	s.UseResume("s-1") // reselecting the current target is a no-op
	if got := s.Generation(); got != 1 {
		t.Fatalf("reselecting the same target advanced the generation: %d", got)
	}

	s.UseResume("s-2")
	if got := s.Generation(); got != 2 {
		t.Fatalf("changing the target generation = %d, want 2", got)
	}
	s.UseResume("s-2")
	if got := s.Generation(); got != 2 {
		t.Fatalf("repeating the same retarget advanced the generation: %d", got)
	}

	s.Reset()
	if got := s.Generation(); got != 3 {
		t.Fatalf("reset generation = %d, want 3", got)
	}
	s.Reset()
	if got := s.Generation(); got != 4 {
		t.Fatalf("second reset generation = %d, want 4", got)
	}
}

func TestCompletedTurnRequiresCleanCurrentGeneration(t *testing.T) {
	const id = "11111111-2222-3333-4444-555555555555"
	tests := []struct {
		name   string
		setup  func(*Model)
		result cake.Result
		want   bool
	}{
		{
			name:   "clean completion",
			setup:  func(m *Model) { m.applyEvent(success(id)); m.sawComplete = true },
			result: cake.Result{ExitCode: 0},
			want:   true,
		},
		{
			name:   "canceled run",
			setup:  func(m *Model) { m.applyEvent(success(id)); m.sawComplete = true },
			result: cake.Result{Canceled: true},
			want:   false,
		},
		{
			name:   "failed run",
			setup:  func(m *Model) { m.applyEvent(success(id)); m.sawComplete = true },
			result: cake.Result{ExitCode: 1},
			want:   false,
		},
		{
			name:   "wait error",
			setup:  func(m *Model) { m.applyEvent(success(id)); m.sawComplete = true },
			result: cake.Result{Err: errors.New("wait failed")},
			want:   false,
		},
		{
			name:   "incomplete run",
			setup:  func(*Model) {},
			result: cake.Result{ExitCode: 0},
			want:   false,
		},
		{
			name:   "errored completion",
			setup:  func(m *Model) { m.applyEvent(failure(id)); m.sawComplete = true },
			result: cake.Result{ExitCode: 0},
			want:   false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newLaidOutModel()
			beginTestRun(&m)
			tt.setup(&m)
			tm, _ := m.finishRun(tt.result)
			got := tm.(Model)
			if got.runBusy() {
				t.Fatal("finishRun left the run busy")
			}
			if got.completedTurn() != tt.want {
				t.Fatalf("completedTurn = %v, want %v (outcome %d, run generation %d, session generation %d)",
					got.completedTurn(), tt.want, got.runOutcome, got.runGeneration, got.session.Generation())
			}
		})
	}
}

func TestAdmissionRefusalMatrix(t *testing.T) {
	activeRun := func(m *Model) { beginTestRun(m) }
	drainingRun := func(m *Model) { beginTestRun(m); m.runOp.cancel() }
	activeCompaction := func(m *Model) {
		m.compactionSeq++
		m.compaction.begin(operationKey{request: m.compactionSeq}, nil, make(chan struct{}))
	}
	drainingCompaction := func(m *Model) {
		activeCompaction(m)
		m.compaction.cancel()
	}

	tests := []struct {
		name          string
		setup         func(*Model)
		prompt        string
		retarget      string
		reason        refusalReason
		promptAllowed bool
	}{
		{name: "idle", setup: func(*Model) {}, reason: refuseNone, promptAllowed: true},
		{
			name:     "exiting",
			setup:    func(m *Model) { m.exitPhase = phaseDraining },
			reason:   refuseExiting,
			prompt:   "",
			retarget: "",
		},
		{
			name:     "browser open",
			setup:    func(m *Model) { m.browser.startLoading() },
			reason:   refuseBrowserOpen,
			prompt:   "close the session browser first",
			retarget: "close the session browser first",
		},
		{
			name:     "run active",
			setup:    activeRun,
			reason:   refuseRunActive,
			prompt:   "a task is already running — Ctrl+C cancels it",
			retarget: "finish or cancel the running task first",
		},
		{
			name:     "run draining",
			setup:    drainingRun,
			reason:   refuseRunDraining,
			prompt:   "the canceled task is still stopping — wait for it to exit",
			retarget: "wait for the canceled task to exit first",
		},
		{
			name:     "replay loading",
			setup:    func(m *Model) { beginTestReplay(m) },
			reason:   refuseReplayLoading,
			prompt:   "waiting for session history to load…",
			retarget: "wait for session history to load first",
		},
		{
			name: "replay draining",
			setup: func(m *Model) {
				beginTestReplay(m)
				m.replay.cancel()
			},
			reason:   refuseReplayDraining,
			prompt:   "waiting for canceled session history to exit…",
			retarget: "wait for canceled session history to exit first",
		},
		{
			name:     "compaction active",
			setup:    activeCompaction,
			reason:   refuseCompactionActive,
			prompt:   "context compaction is running — wait for it or press Ctrl+C to cancel",
			retarget: "wait for context compaction to finish (Ctrl+C cancels it)",
		},
		{
			name:     "compaction draining",
			setup:    drainingCompaction,
			reason:   refuseCompactionDraining,
			prompt:   "canceled context compaction is still stopping — wait for it to exit",
			retarget: "wait for canceled context compaction to exit first",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newLaidOutModel()
			tt.setup(&m)
			prompt := m.admitPrompt()
			retarget := m.admitRetarget()
			if prompt.allowed != tt.promptAllowed {
				t.Fatalf("admitPrompt allowed = %v, want %v", prompt.allowed, tt.promptAllowed)
			}
			if retarget.allowed != tt.promptAllowed {
				t.Fatalf("admitRetarget allowed = %v, want %v", retarget.allowed, tt.promptAllowed)
			}
			if prompt.reason != tt.reason || retarget.reason != tt.reason {
				t.Fatalf("reasons = %v/%v, want %v", prompt.reason, retarget.reason, tt.reason)
			}
			if prompt.text != tt.prompt {
				t.Errorf("prompt text = %q, want %q", prompt.text, tt.prompt)
			}
			if retarget.text != tt.retarget {
				t.Errorf("retarget text = %q, want %q", retarget.text, tt.retarget)
			}
		})
	}
}

func TestCtrlNDuringRunKeepsGateClosedUntilReaped(t *testing.T) {
	const oldID = "11111111-2222-3333-4444-555555555555"
	m := newLaidOutModel()
	oldKey := beginTestRun(&m)
	m.session.OnTaskStart(cake.TaskStart{SessionID: oldID, TaskID: "t-1"})
	oldGeneration := m.session.Generation()

	tm, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlN})
	m = tm.(Model)
	requireTitle(t, cmd, windowTitle(m.cfg.Cwd, "[stopping]"))
	if !m.runOp.draining() {
		t.Fatal("Ctrl+N did not put the run into its drain phase")
	}
	if m.session.Generation() <= oldGeneration {
		t.Fatalf("Ctrl+N did not advance the generation: %d -> %d", oldGeneration, m.session.Generation())
	}

	m.input.SetValue("after the boundary")
	tm, cmd = m.submit()
	m = tm.(Model)
	if cmd != nil {
		t.Fatalf("prompt during the run drain returned a command: %v", cmd)
	}
	if m.input.Value() != "after the boundary" {
		t.Fatalf("prompt during the drain was not preserved: %q", m.input.Value())
	}
	if it := lastItem(t, m); it.Kind != ui.KindWarning ||
		!strings.Contains(it.Text, "canceled task is still stopping") {
		t.Fatalf("prompt drain refusal = %+v", it)
	}

	for _, tt := range []struct {
		name  string
		input string
	}{
		{"new", "/new"},
		{"resume", "/resume aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"},
		{"sessions", "/sessions"},
	} {
		m.input.SetValue(tt.input)
		tm, cmd = m.submit()
		m = tm.(Model)
		if cmd != nil {
			t.Fatalf("%s during the run drain returned a command: %v", tt.name, cmd)
		}
		if it := lastItem(t, m); it.Kind != ui.KindWarning ||
			!strings.Contains(it.Text, "wait for the canceled task to exit first") {
			t.Fatalf("%s drain refusal = %+v", tt.name, it)
		}
		if m.session.Generation() != oldGeneration+1 {
			t.Fatalf("%s changed the generation during the drain", tt.name)
		}
		if m.session.ResumeID != "" || m.browser.open() {
			t.Fatalf("%s retargeted the session during the drain: resume=%q browser=%v", tt.name, m.session.ResumeID, m.browser.open())
		}
	}

	// A late old-run event must not enter the fresh timeline. The refusal
	// warnings above stay visible; the old assistant output must not.
	tm, _ = m.Update(eventsMsg{key: oldKey, evs: []cake.Event{
		cake.Message{Role: "assistant", Content: "late old output"},
	}})
	m = tm.(Model)
	for _, it := range m.timeline.items {
		if it.Text == "late old output" {
			t.Fatalf("late event crossed the boundary: %#v", m.timeline.items)
		}
	}
	if len(m.timeline.items) == 0 || m.timeline.items[0].Text != "New session" {
		t.Fatalf("new-session boundary missing: %#v", m.timeline.items)
	}

	tm, cmd = m.finishRunKey(oldKey, cake.Result{Canceled: true})
	m = tm.(Model)
	requireTitle(t, cmd, "cake-repl: ")
	if m.runBusy() {
		t.Fatal("the reaped run left the gate closed")
	}
	if mode, id := m.session.RunOptions(); mode != cake.RunFresh || id != "" {
		t.Fatalf("the canceled run re-pinned the new session: %v %q", mode, id)
	}
	for _, it := range m.timeline.items {
		if it.Text == "canceled" {
			t.Fatalf("the canceled result crossed the boundary: %#v", m.timeline.items)
		}
	}
	if m.completedTurn() {
		t.Fatal("the stale canceled result counted as a completed turn")
	}

	// The gate opens only after the result, and a new prompt starts normally.
	m.cfg.CakeBin = writeFakeCake(t, "exit 0\n")
	m.cfg.Cwd = t.TempDir()
	m.input.SetValue("accepted prompt")
	tm, cmd = m.submit()
	m = tm.(Model)
	if !m.runBusy() || cmd == nil {
		t.Fatal("prompt was not accepted after the run was reaped")
	}
	if it := lastItem(t, m); it.Kind != ui.KindUser || it.Text != "accepted prompt" {
		t.Fatalf("accepted prompt item = %+v", it)
	}
	drainRun(t, m.run)
	tm, _ = m.finishRunKey(m.runOp.key, cake.Result{ExitCode: 0})
	_ = tm
}

func TestCtrlCCancelsRunIntoDrain(t *testing.T) {
	m := newLaidOutModel()
	key := beginTestRun(&m)
	canceled := false
	m.runOp.cancelFn = func() { canceled = true }

	tm, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlC})
	m = tm.(Model)
	requireTitle(t, cmd, windowTitle(m.cfg.Cwd, "[stopping]"))
	if !canceled {
		t.Fatal("Ctrl+C did not ask the run child to stop")
	}
	if !m.runOp.draining() || !m.runBusy() {
		t.Fatal("Ctrl+C did not retain the draining run phase")
	}

	m.input.SetValue("during the drain")
	tm, cmd = m.submit()
	m = tm.(Model)
	if cmd != nil || m.input.Value() != "during the drain" {
		t.Fatalf("prompt during the drain: cmd=%v input=%q", cmd, m.input.Value())
	}
	if it := lastItem(t, m); it.Kind != ui.KindWarning ||
		!strings.Contains(it.Text, "canceled task is still stopping") {
		t.Fatalf("prompt drain refusal = %+v", it)
	}

	tm, cmd = m.execCommand(Command{Kind: CmdNew})
	m = tm.(Model)
	if cmd != nil {
		t.Fatalf("/new during the drain returned %v", cmd)
	}
	if it := lastItem(t, m); it.Kind != ui.KindWarning ||
		!strings.Contains(it.Text, "wait for the canceled task to exit first") {
		t.Fatalf("/new drain refusal = %+v", it)
	}

	tm, cmd = m.finishRunKey(key, cake.Result{Canceled: true})
	m = tm.(Model)
	requireTitle(t, cmd, "cake-repl: ")
	if m.runBusy() {
		t.Fatal("the reaped cancellation left the run phase busy")
	}
	if m.runOutcome != outcomeCanceled {
		t.Fatalf("run outcome = %d, want canceled", m.runOutcome)
	}
	if gate := m.admitPrompt(); !gate.allowed {
		t.Fatalf("prompt gate stayed closed after the canceled run was reaped: %+v", gate)
	}
}

func TestStaleRunResultCannotMutateNewSession(t *testing.T) {
	const oldID = "11111111-2222-3333-4444-555555555555"
	m := newLaidOutModel()
	oldKey := beginTestRun(&m)
	m.applyEvent(cake.TaskStart{SessionID: oldID, TaskID: "t-1"})
	m.applyEvent(cake.TurnUsage{
		Usage:         cake.Usage{InputTokens: 170_000},
		UsagePresence: "complete",
		ContextWindow: 200_000,
	})
	m.applyEvent(success(oldID))
	m.sawComplete = true

	tm, _ := m.startNewSession()
	m = tm.(Model)
	if m.session.SessionID != "" || m.session.LastComplete != nil || m.session.Context != nil {
		t.Fatalf("new-session boundary kept old identity: %+v", m.session)
	}

	tm, cmd := m.finishRunKey(oldKey, cake.Result{ExitCode: 0})
	m = tm.(Model)
	requireTitle(t, cmd, "cake-repl: ")
	if m.runBusy() {
		t.Fatal("the stale result did not settle the run phase")
	}
	if m.session.SessionID != "" || m.session.LastComplete != nil || m.session.Context != nil {
		t.Fatalf("the stale result mutated the new session: %+v", m.session)
	}
	if mode, id := m.session.RunOptions(); mode != cake.RunFresh || id != "" {
		t.Fatalf("the stale result retargeted the new session: %v %q", mode, id)
	}
	if len(m.timeline.items) != 1 || m.timeline.items[0].Text != "New session" {
		t.Fatalf("the stale result entered the new timeline: %#v", m.timeline.items)
	}
	if m.completedTurn() {
		t.Fatal("the stale result counted as a completed turn")
	}
}

func TestRunAndCompactionCannotOverlap(t *testing.T) {
	const id = "11111111-2222-3333-4444-555555555555"
	m := laidOut(160, 24)
	m.cfg.Compaction = CompactionConfig{
		Enabled:          true,
		Bin:              "cake-compact",
		ThresholdPercent: 80,
		KeepTokens:       20_000,
	}
	m.session.OnTaskStart(cake.TaskStart{SessionID: id, TaskID: "t-1"})
	m.applyEvent(cake.TurnUsage{
		Usage:         cake.Usage{InputTokens: 170_000},
		UsagePresence: "complete",
		ContextWindow: 200_000,
	})
	m.applyEvent(success(id))
	beginTestRun(&m)

	// A run in flight blocks compaction.
	if m.shouldCompactAfter() {
		t.Fatal("a run in flight was considered compaction-eligible")
	}
	if next, cmd := m.startCompaction(); cmd != nil || next.compactionBusy() {
		t.Fatal("startCompaction started while a run was in flight")
	}

	// Finishing the run starts compaction; the two phases never overlap.
	tm, cmd := m.finishRun(cake.Result{ExitCode: 0})
	m = tm.(Model)
	if cmd == nil || !m.compactionBusy() || m.runBusy() {
		t.Fatalf("finishRun overlap state: cmd nil=%v compacting=%v running=%v", cmd == nil, m.compactionBusy(), m.runBusy())
	}

	// Compaction in flight blocks a new run.
	runSeq := m.runSeq
	tm, cmd = m.startRun("blocked")
	m = tm.(Model)
	if cmd != nil || m.runBusy() || m.runSeq != runSeq {
		t.Fatal("startRun started while compaction was in flight")
	}

	// Settle the synthetic compactor so the test leaves no live phase.
	tm, _ = m.finishCompaction(compactionDoneMsg{
		req:       m.compaction.key.request,
		sessionID: id,
		res:       compact.Result{Canceled: true},
	})
	m = tm.(Model)
	if m.compactionBusy() || m.runBusy() {
		t.Fatal("compaction settle left an operation busy")
	}
}
