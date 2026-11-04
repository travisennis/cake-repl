package app

import "context"

// phase is the lifecycle of one model-owned request. Completion is not a
// phase: once a result is settled the request is idle, and the caller decides
// what the result means.
type phase uint8

const (
	phaseIdle phase = iota
	phaseInFlight
	phaseDraining
)

// sessionGeneration identifies continuity of the session target. It advances
// only when the user leaves the current target (/new, Ctrl+N, or a /resume pick
// that actually changes the target), never when a run merely reports its own id.
type sessionGeneration uint64

// outcome is what a completed request did. It is not a lifecycle phase: a
// phase says whether a child still holds a resource, while the outcome says
// whether the result may drive a later action.
type outcome uint8

const (
	outcomeNone outcome = iota
	outcomeSuccess
	outcomeFailure
	outcomeCanceled
)

// operationKey identifies one request. The generation scopes the request to the
// session target it started for; request is per operation and never repeats;
// sessionID is the target session when the request has one. A stale generation
// suppresses application, but the caller still settles the phase so the
// resource gate opens once the child is reaped.
type operationKey struct {
	generation sessionGeneration
	request    uint64
	sessionID  string
}

func (k operationKey) zero() bool {
	return k == operationKey{}
}

// admission is one capability's gate decision. A refusal carries the exact
// text for that capability, so a prompt and a session command can share the
// same blocking phase without sharing the user-facing wording.
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
	refuseCompactionActive
	refuseCompactionDraining
)

// pendingOperation owns the request identity, cancel function, and reaping
// channel for one model-owned child. It deliberately knows nothing about input
// gates or presentation: callers decide whether an active or draining operation
// blocks a prompt, a session retarget, or a new request.
type pendingOperation struct {
	phase    phase
	key      operationKey
	drainKey operationKey
	cancelFn context.CancelFunc
	done     <-chan struct{}
}

// begin starts one request. Callers start a request only after the previous
// one has settled; a begin therefore replaces any stale zero value left by a
// test or a zero-value Model.
func (o *pendingOperation) begin(key operationKey, cancel context.CancelFunc, done <-chan struct{}) {
	o.phase = phaseInFlight
	o.key = key
	o.drainKey = operationKey{}
	o.cancelFn = cancel
	o.done = done
}

// cancel asks the child to stop and keeps its key. A run, replay, or compaction
// still produces an outcome after cancellation, so its result must settle the
// operation and be classified by the caller.
func (o *pendingOperation) cancel() {
	if o.phase == phaseIdle {
		return
	}
	o.phase = phaseDraining
	o.callCancel()
}

// invalidate asks the child to stop and moves its key out of the active slot.
// A late suggestion or listing result may only close the drain; it can never
// apply to the composer or browser after the user's newer action.
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

// settle consumes the reaping result for key. It reports true only for the
// active key, whose result may still be applied; an invalidated request returns
// false and only clears its drain. An unknown key leaves the operation exactly
// as it was, so a superseded result cannot settle a newer request.
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

// doneChannel returns the reaping channel while a child is active or draining,
// or nil when the operation is idle. Shutdown waits on this channel instead of
// the result message.
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

// runBusy reports whether a run holds a resource that gates prompt submission
// and session retargeting. Both an active and a draining run stay busy until its
// result is settled.
func (m Model) runBusy() bool {
	return m.runOp.active() || m.runOp.draining()
}

// runActive reports whether a run is still in flight (not merely draining).
func (m Model) runActive() bool {
	return m.runOp.active()
}

// replayBusy reports whether startup hydration still owns a child. Replay is
// typed as its own phase by task 139; until then these fields are the same
// loading/draining resource gate.
func (m Model) replayBusy() bool {
	return m.hydrating || m.replayPending || m.replayRun != nil
}

// exiting reports whether the exit phase is draining. Input is ignored from
// that point until every child is reaped.
func (m Model) exiting() bool {
	return m.exitPhase == phaseDraining
}

// completedTurn is the one answer both post-turn operations use. A clean
// completed turn is an outcomeSuccess result for the current session target;
// canceled, failed, incomplete, or superseded runs never qualify.
func (m Model) completedTurn() bool {
	return m.runOutcome == outcomeSuccess && m.runGeneration == m.session.Generation()
}

// admitPrompt and admitRetarget are the two capability-specific uses of the
// same gate. They check the same resource-busy phases but return the distinct
// refusal wording each caller already showed.
func (m Model) admitPrompt() admission {
	return m.admit(false)
}

func (m Model) admitRetarget() admission {
	return m.admit(true)
}

func (m Model) admit(retarget bool) admission {
	if m.exiting() {
		return admission{reason: refuseExiting}
	}
	if m.browserOpen {
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
	if m.replayBusy() {
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
