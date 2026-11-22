package app

import (
	"context"
	"errors"
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/travisennis/cake-repl/internal/cake"
	"github.com/travisennis/cake-repl/internal/ui"
)

type replayPhase uint8

const (
	replayIdle replayPhase = iota
	replayLoading
	replayDraining
)

type replayState struct {
	op     pendingOperation
	seq    uint64
	ctx    context.Context
	done   chan struct{}
	run    *cake.Run
	warned bool
}

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

func (r *replayState) clear() {
	r.ctx = nil
	r.done = nil
	r.run = nil
}

func (r replayState) refusalReason() refusalReason {
	if r.phase() == replayDraining {
		return refuseReplayDraining
	}
	return refuseReplayLoading
}

type replayStartedMsg struct {
	key operationKey
	run *cake.Run
	err error
}

type replayEventsMsg struct {
	key operationKey
	evs []cake.Event
}

type replayDoneMsg struct {
	key operationKey
	res cake.Result
}

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
		trustCmd := m.startDeferredTrustProbeIfIdle()
		if current {
			err := msg.err
			if err == nil {
				err = errors.New("replay did not start")
			}
			m.replay.warned = true
			m.appendItem(ui.Item{Kind: ui.KindWarning, Text: "session replay unavailable: " + err.Error()})
			m.syncViewport()
		}
		next, cmd := m.afterPresentationChange()
		if trustCmd != nil {
			cmd = tea.Batch(cmd, trustCmd)
		}
		return next, cmd
	}

	m.replay.run = msg.run
	if m.exiting() || m.replay.phase() != replayLoading || !current {
		msg.run.Cancel()
	}
	return m, waitForReplay(msg.run, msg.key)
}

func (m Model) handleReplayEvents(msg replayEventsMsg) (tea.Model, tea.Cmd) {
	if m.replay.run == nil || m.replay.op.key != msg.key {
		return m, nil
	}
	cwdRestored := false
	if m.replay.phase() == replayLoading &&
		msg.key.generation == m.session.Generation() &&
		msg.key.sessionID == m.session.ResumeID {
		for _, ev := range msg.evs {
			if m.applyReplayEvent(ev) {
				cwdRestored = true
			}
		}
		m.syncViewport()
	}
	cmd := waitForReplay(m.replay.run, msg.key)
	if cwdRestored {
		cmd = tea.Batch(cmd, m.titleCmd())
		if trustCmd := m.startDeferredTrustProbe(); trustCmd != nil {
			cmd = tea.Batch(cmd, trustCmd)
		}
	}
	return m, cmd
}

func (m Model) finishReplay(msg replayDoneMsg) (tea.Model, tea.Cmd) {
	if !m.replay.settle(msg.key) {
		return m.maybeQuit()
	}
	m.replay.clear()
	trustCmd := m.startDeferredTrustProbeIfIdle()
	current := msg.key.generation == m.session.Generation() &&
		msg.key.sessionID == m.session.ResumeID
	if !current {
		next, cmd := m.afterPresentationChange()
		if trustCmd != nil {
			cmd = tea.Batch(cmd, trustCmd)
		}
		return next, cmd
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
				text += "\n" + ui.TrimForDisplay(msg.res.Stderr, m.cfg.OutputLimit)
			}
			m.appendItem(ui.Item{Kind: ui.KindWarning, Text: text})
		}
	}
	hydratedCompletion := !msg.res.Canceled && msg.res.Err == nil && msg.res.ExitCode == 0 &&
		!m.replay.warned && m.sawComplete && m.session.LastComplete != nil &&
		!m.session.LastComplete.IsError
	if hydratedCompletion {
		m.suggestion.hydrated = true
	}
	m.finishPendingTools()
	m.syncViewport()
	next, cmd := m.afterPresentationChange()
	if trustCmd != nil {
		cmd = tea.Batch(cmd, trustCmd)
	}
	if !hydratedCompletion {
		return next, cmd
	}
	next, suggestionCmd := next.maybeStartSuggestion()
	if suggestionCmd == nil {
		return next, cmd
	}
	return next, tea.Batch(cmd, suggestionCmd, next.spin.Tick)
}
