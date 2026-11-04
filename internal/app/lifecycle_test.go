package app

import "testing"

func TestPendingOperationCancelKeepsResultUntilSettled(t *testing.T) {
	var op pendingOperation
	done := make(chan struct{})
	cancels := 0
	key := operationKey{request: 7}

	op.begin(key, func() { cancels++ }, done)
	if !op.active() || op.draining() {
		t.Fatalf("begin = active:%v draining:%v, want active", op.active(), op.draining())
	}

	op.cancel()
	if op.active() || !op.draining() {
		t.Fatalf("cancel = active:%v draining:%v, want draining", op.active(), op.draining())
	}
	if cancels != 1 {
		t.Fatalf("cancel calls = %d, want 1", cancels)
	}

	// A superseded result must not settle the canceled request.
	if op.settle(operationKey{request: 8}) {
		t.Fatal("a superseded key settled the operation")
	}
	if !op.draining() {
		t.Fatal("a superseded key changed the drain state")
	}
	if op.settle(operationKey{}) {
		t.Fatal("the zero key settled the operation")
	}

	// The canceled request still has an outcome to classify, so its key settles
	// it and reports a result.
	if !op.settle(key) {
		t.Fatal("the canceled key did not settle the operation")
	}
	if op.active() || op.draining() || op.doneChannel() != nil {
		t.Fatalf("settled operation = active:%v draining:%v done:%v", op.active(), op.draining(), op.doneChannel() != nil)
	}
}

func TestPendingOperationInvalidateDropsResultAndClosesDrain(t *testing.T) {
	var op pendingOperation
	done := make(chan struct{})
	cancels := 0
	key := operationKey{request: 3}

	op.begin(key, func() { cancels++ }, done)
	op.invalidate()
	if op.active() || !op.draining() {
		t.Fatalf("invalidate = active:%v draining:%v, want draining", op.active(), op.draining())
	}
	if cancels != 1 {
		t.Fatalf("invalidate calls = %d, want 1", cancels)
	}

	// An invalidated result may close the drain, but it must never apply.
	if op.settle(key) {
		t.Fatal("an invalidated result was reported as applicable")
	}
	if op.active() || op.draining() || op.doneChannel() != nil {
		t.Fatalf("settled invalidated operation = active:%v draining:%v done:%v", op.active(), op.draining(), op.doneChannel() != nil)
	}
}

func TestPendingOperationUnknownKeyLeavesActiveRequest(t *testing.T) {
	var op pendingOperation
	done := make(chan struct{})
	key := operationKey{request: 11}
	op.begin(key, func() {}, done)

	if op.settle(operationKey{request: 12}) {
		t.Fatal("an unknown key settled the active request")
	}
	if !op.active() || op.draining() {
		t.Fatalf("unknown key changed state = active:%v draining:%v", op.active(), op.draining())
	}
	if !op.settle(key) {
		t.Fatal("the active key did not settle the request")
	}
}

func TestPendingOperationCancelIsIdempotent(t *testing.T) {
	var op pendingOperation
	cancels := 0
	op.begin(operationKey{request: 1}, func() { cancels++ }, make(chan struct{}))

	op.cancel()
	op.cancel()
	if cancels != 1 {
		t.Fatalf("repeated cancel calls = %d, want 1", cancels)
	}
	if !op.draining() {
		t.Fatal("repeated cancel left the operation not draining")
	}
}
