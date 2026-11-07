package app

import (
	"context"
	"errors"
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/travisennis/cake-repl/internal/cake"
	"github.com/travisennis/cake-repl/internal/ui"
)

// replayPhase is the one lifecycle state for the startup replay. It covers the
// start command, record application, the reaped result, and the post-boundary
// drain. A replay is read-only, but it still owns a child and gates prompt
// submission and retargeting until that child is reaped.
type replayPhase uint8

const (
	replayIdle replayPhase = iota
	replayLoading
	replayDraining
)

// replayState owns the startup replay request and its subprocess. The pending
// operation supplies the phase, request key, cancellation, and reaping channel;
// run is the handle once cake.Replay has started, and warned keeps the warning
// path to one visible message per replay. ctx and done are the startup
// command's handles, so an unobserved exit can stop a replay that has not
// delivered replayStartedMsg yet.
type replayState struct {
	op     pendingOperation
	seq    uint64
	ctx    context.Context
	done   chan struct{}
	run    *cake.Run
	warned bool
}

// phase maps the common pending-operation lifecycle to the replay vocabulary.
// replayLoading includes both the start command and record application: those
// are one live request, not separate resource states.
func (r replayState) phase() replayPhase {
	switch {
	case r.op.active():
		return replayLoading
	case r.op.draining():
		return replayDraining
	default:
		return replayIdle
	}
}

// begin allocates one generation-scoped replay request. A replay starts only at
// startup, so it is the only caller and its key identifies the records and
// result that belong to this hydration.
func (r *replayState) begin(ctx context.Context, generation sessionGeneration, sessionID string, cancel context.CancelFunc, done chan struct{}) operationKey {
	r.seq++
	key := operationKey{generation: generation, request: r.seq, sessionID: sessionID}
	r.op.begin(key, cancel, done)
	r.ctx = ctx
	r.done = done
	r.run = nil
	r.warned = false
	return key
}

// cancel stops the replay whether it is still starting or already running.
// The pending operation keeps its key so the reaped result can settle the
// drain and release the gate.
func (r *replayState) cancel() {
	r.op.cancel()
	if r.run != nil {
		r.run.Cancel()
	}
}

func (r *replayState) settle(key operationKey) bool {
	return r.op.settle(key)
}

func (r *replayState) doneChannel() <-chan struct{} {
	return r.op.doneChannel()
}

// clear drops the startup-command handles after the replay has settled. warned
// deliberately remains for the finish path that is about to classify the
// result.
func (r *replayState) clear() {
	r.ctx = nil
	r.done = nil
	r.run = nil
}

// refusalReason names the replay gate's phase for callers that do not display
// the refusal. The caller that does display it chooses the matching wording.
func (r replayState) refusalReason() refusalReason {
	if r.phase() == replayDraining {
		return refuseReplayDraining
	}
	return refuseReplayLoading
}

// replayStartedMsg reports the subprocess created for startup hydration. key
// scopes the start and its eventual stream to the request that started it.
type replayStartedMsg struct {
	key operationKey
	run *cake.Run
	err error
}

// replayEventsMsg delivers a batch of replay records in stream order. key
// scopes the batch so a stream started before a session boundary cannot apply
// its records after the user leaves that session.
type replayEventsMsg struct {
	key operationKey
	evs []cake.Event
}

// replayDoneMsg delivers the terminal state of a replay subprocess.
type replayDoneMsg struct {
	key operationKey
	res cake.Result
}

// startReplayCmd starts the read-only replay without making the live prompt
// state look busy. The command runs outside the Update loop because starting a
// subprocess can block briefly. The model-owned context lets an unobserved exit
// stop a replay that has not yet delivered replayStartedMsg; done closes after
// the child is reaped even if that message is never observed.
func startReplayCmd(cfg Config, key operationKey, ctx context.Context, done chan struct{}) tea.Cmd {
	if ctx == nil {
		ctx = context.Background()
	}
	return func() tea.Msg {
		run, err := cake.Replay(cake.ReplayOptions{
			Bin:       cfg.CakeBin,
			Cwd:       cfg.Cwd,
			SessionID: key.sessionID,
			DebugLog:  cfg.DebugLog,
			Registry:  cfg.Runs,
			Context:   ctx,
		})
		if run == nil {
			if done != nil {
				close(done)
			}
			return replayStartedMsg{key: key, run: nil, err: err}
		}
		if done != nil {
			go func() {
				<-run.Done
				close(done)
			}()
		}
		return replayStartedMsg{key: key, run: run, err: err}
	}
}

// waitForReplay pulls the next batch of events from a replay run, or its final
// result once the event stream is closed. It mirrors waitForRun but keeps
// hydration separate from a live prompt run.
func waitForReplay(run *cake.Run, key operationKey) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-run.Events
		if !ok {
			return replayDoneMsg{key: key, res: <-run.Result}
		}
		evs := []cake.Event{ev}
		for len(evs) < maxEventsPerBatch {
			select {
			case ev, ok := <-run.Events:
				if !ok {
					return replayEventsMsg{key: key, evs: evs}
				}
				evs = append(evs, ev)
			default:
				return replayEventsMsg{key: key, evs: evs}
			}
		}
		return replayEventsMsg{key: key, evs: evs}
	}
}

// finishReplayStart handles the start of the replay subprocess. A start failure
// is visibly non-fatal unless the request already belongs to a session the user
// left; a start after Ctrl+N or quit is canceled and still drained.
func (m Model) finishReplayStart(msg replayStartedMsg) (tea.Model, tea.Cmd) {
	if m.replay.op.key != msg.key {
		if msg.run != nil {
			msg.run.Cancel()
		}
		return m, nil
	}
	current := msg.key.generation == m.session.Generation() &&
		msg.key.sessionID == m.session.ResumeID
	if msg.err != nil || msg.run == nil {
		if msg.run != nil {
			msg.run.Cancel()
		}
		m.replay.settle(msg.key)
		m.replay.clear()
		if current {
			err := msg.err
			if err == nil {
				err = errors.New("replay did not start")
			}
			m.replay.warned = true
			m.appendItem(ui.Item{Kind: ui.KindWarning, Text: "session replay unavailable: " + err.Error()})
			m.syncViewport()
		}
		return m.afterPresentationChange()
	}

	m.replay.run = msg.run
	if m.exiting() || m.replay.phase() != replayLoading || !current {
		// The user left or started exiting before the child handle arrived. Keep
		// consuming it so the drain can settle, but stop applying its records.
		msg.run.Cancel()
	}
	return m, waitForReplay(msg.run, msg.key)
}

// handleReplayEvents applies one batch only while hydration is live and the
// request still belongs to the current session target. A post-boundary drain
// continues consuming records so the child is reaped, but none may enter the
// new timeline.
func (m Model) handleReplayEvents(msg replayEventsMsg) (tea.Model, tea.Cmd) {
	if m.replay.run == nil || m.replay.op.key != msg.key {
		return m, nil
	}
	if m.replay.phase() == replayLoading &&
		msg.key.generation == m.session.Generation() &&
		msg.key.sessionID == m.session.ResumeID {
		for _, ev := range msg.evs {
			m.applyReplayEvent(ev)
		}
		m.syncViewport()
	}
	return m, waitForReplay(m.replay.run, msg.key)
}

// finishReplay applies one replay result. The phase settles before the result is
// classified so a canceled replay can open the gate; a result from a session
// the user left is dropped without its records, warning, or context reaching
// the new session.
func (m Model) finishReplay(msg replayDoneMsg) (tea.Model, tea.Cmd) {
	if !m.replay.settle(msg.key) {
		return m.maybeQuit()
	}
	m.replay.clear()
	current := msg.key.generation == m.session.Generation() &&
		msg.key.sessionID == m.session.ResumeID
	if !current {
		return m.afterPresentationChange()
	}

	if !m.replay.warned {
		switch {
		case msg.res.Canceled:
			m.appendItem(ui.Item{Kind: ui.KindWarning, Text: "session replay canceled"})
		case msg.res.Err != nil:
			m.appendItem(ui.Item{Kind: ui.KindWarning, Text: "session replay failed: " + msg.res.Err.Error()})
		case msg.res.ExitCode != 0:
			text := fmt.Sprintf("session replay failed with exit code %d", msg.res.ExitCode)
			if msg.res.Stderr != "" {
				text += "\n" + ui.TruncateOutput(msg.res.Stderr, m.cfg.OutputLimit)
			}
			m.appendItem(ui.Item{Kind: ui.KindWarning, Text: text})
		}
	}
	// A clean replay that ends at a completed, non-errored turn is the startup
	// evidence for one suggestion after the replay child is reaped. It is a
	// suggestion-only flag: hydration must not write the live run outcome or
	// start compaction. A replay task_start clears sawComplete, so an earlier
	// completion cannot qualify a session whose final turn is incomplete. See
	// ADR 037.
	hydratedCompletion := !msg.res.Canceled && msg.res.Err == nil && msg.res.ExitCode == 0 &&
		!m.replay.warned && m.sawComplete && m.session.LastComplete != nil &&
		!m.session.LastComplete.IsError
	if hydratedCompletion {
		m.suggestion.hydrated = true
	}
	m.finishPendingTools()
	m.syncViewport()
	next, cmd := m.afterPresentationChange()
	if !hydratedCompletion {
		return next, cmd
	}
	next, suggestionCmd := next.maybeStartSuggestion()
	if suggestionCmd == nil {
		return next, cmd
	}
	return next, tea.Batch(cmd, suggestionCmd)
}
