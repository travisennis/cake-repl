package app

import "context"

type phase uint8

const (
	phaseIdle phase = iota
	phaseInFlight
	phaseDraining
)

type sessionGeneration uint64

type outcome uint8

const (
	outcomeNone outcome = iota
	outcomeSuccess
	outcomeFailure
	outcomeCanceled
)

type operationKey struct {
	generation sessionGeneration
	request    uint64
	sessionID  string
}

func (k operationKey) zero() bool {
	return k == operationKey{}
}

type admission struct {
	allowed bool
	reason  refusalReason
	text    string
}

type refusalReason uint8

const (
	refuseNone refusalReason = iota
	refuseExiting
	refuseBrowserOpen
	refuseRunActive
	refuseRunDraining
	refuseReplayLoading
	refuseReplayDraining
	refuseCompactionActive
	refuseCompactionDraining
)

type pendingOperation struct {
	phase    phase
	key      operationKey
	drainKey operationKey
	cancelFn context.CancelFunc
	done     <-chan struct{}
}

func (o *pendingOperation) begin(key operationKey, cancel context.CancelFunc, done <-chan struct{}) {
	o.phase = phaseInFlight
	o.key = key
	o.drainKey = operationKey{}
	o.cancelFn = cancel
	o.done = done
}

func (o *pendingOperation) cancel() {
	if o.phase == phaseIdle {
		return
	}
	o.phase = phaseDraining
	o.callCancel()
}

func (o *pendingOperation) invalidate() {
	if o.phase == phaseIdle {
		return
	}
	if !o.key.zero() {
		o.drainKey = o.key
		o.key = operationKey{}
	}
	o.phase = phaseDraining
	o.callCancel()
}

func (o *pendingOperation) active() bool {
	return o.phase == phaseInFlight
}

func (o *pendingOperation) draining() bool {
	return o.phase == phaseDraining
}

func (o *pendingOperation) settle(key operationKey) bool {
	if key.zero() {
		return false
	}
	if !o.key.zero() && o.key == key {
		o.clear()
		return true
	}
	if !o.drainKey.zero() && o.drainKey == key {
		o.drainKey = operationKey{}
		o.clearReap()
		if o.key.zero() {
			o.phase = phaseIdle
		}
		return false
	}
	return false
}

func (o *pendingOperation) doneChannel() <-chan struct{} {
	return o.done
}

func (o *pendingOperation) callCancel() {
	if o.cancelFn != nil {
		o.cancelFn()
		o.cancelFn = nil
	}
}

func (o *pendingOperation) clear() {
	o.callCancel()
	o.phase = phaseIdle
	o.key = operationKey{}
	o.drainKey = operationKey{}
	o.clearReap()
}

func (o *pendingOperation) clearReap() {
	o.cancelFn = nil
	o.done = nil
}

func (m Model) runBusy() bool {
	return m.runOp.active() || m.runOp.draining()
}

func (m Model) runActive() bool {
	return m.runOp.active()
}

func (m Model) replayBusy() bool {
	return m.replay.phase() != replayIdle
}

func (m Model) exiting() bool {
	return m.exitPhase == phaseDraining
}

func (m Model) completedTurn() bool {
	return m.runOutcome == outcomeSuccess && m.runGeneration == m.session.Generation()
}

func (m Model) admitPrompt() admission {
	return m.admit(false)
}

func (m Model) admitRetarget() admission {
	return m.admit(true)
}

func (m Model) admitSuggestion() admission {
	if m.exiting() {
		return admission{reason: refuseExiting}
	}
	if m.browser.open() {
		return admission{reason: refuseBrowserOpen}
	}
	if m.runBusy() {
		return admission{reason: refuseRunActive}
	}
	if m.replayBusy() {
		return admission{reason: m.replay.refusalReason()}
	}
	return admission{allowed: true, reason: refuseNone}
}

func (m Model) admit(retarget bool) admission {
	if m.exiting() {
		return admission{reason: refuseExiting}
	}
	if m.browser.open() {
		return admission{reason: refuseBrowserOpen, text: "close the session browser first"}
	}
	if m.runActive() {
		if retarget {
			return admission{reason: refuseRunActive, text: "finish or cancel the running task first"}
		}
		return admission{reason: refuseRunActive, text: "a task is already running — Ctrl+C cancels it"}
	}
	if m.runOp.draining() {
		if retarget {
			return admission{reason: refuseRunDraining, text: "wait for the canceled task to exit first"}
		}
		return admission{reason: refuseRunDraining, text: "the canceled task is still stopping — wait for it to exit"}
	}
	if p := m.replay.phase(); p != replayIdle {
		if p == replayDraining {
			if retarget {
				return admission{reason: refuseReplayDraining, text: "wait for canceled session history to exit first"}
			}
			return admission{reason: refuseReplayDraining, text: "waiting for canceled session history to exit…"}
		}
		if retarget {
			return admission{reason: refuseReplayLoading, text: "wait for session history to load first"}
		}
		return admission{reason: refuseReplayLoading, text: "waiting for session history to load…"}
	}
	if m.compaction.active() {
		if retarget {
			return admission{reason: refuseCompactionActive, text: "wait for context compaction to finish (Ctrl+C cancels it)"}
		}
		return admission{reason: refuseCompactionActive, text: "context compaction is running — wait for it or press Ctrl+C to cancel"}
	}
	if m.compaction.draining() {
		if retarget {
			return admission{reason: refuseCompactionDraining, text: "wait for canceled context compaction to exit first"}
		}
		return admission{reason: refuseCompactionDraining, text: "canceled context compaction is still stopping — wait for it to exit"}
	}
	return admission{allowed: true, reason: refuseNone}
}
