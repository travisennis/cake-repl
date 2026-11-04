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

// operationKey identifies one request. The request counter is per operation
// and never repeats. The run/replay migration adds the session generation and
// target session to this key; compaction and suggestion only need the counter
// today.
type operationKey struct {
	request uint64
}

func (k operationKey) zero() bool {
	return k == operationKey{}
}

// pendingOperation owns the request identity, cancel function, and reaping
// channel for one model-owned child. It deliberately knows nothing about input
// gates or presentation: callers decide whether an active or draining operation
// blocks a prompt, a session retarget, or a new request.
type pendingOperation struct {
	phase    phase
	key      operationKey
	drainKey operationKey
	cancelFn context.CancelFunc
	done     chan struct{}
}

// begin starts one request. Callers start a request only after the previous
// one has settled; a begin therefore replaces any stale zero value left by a
// test or a zero-value Model.
func (o *pendingOperation) begin(key operationKey, cancel context.CancelFunc, done chan struct{}) {
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
