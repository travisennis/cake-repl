package app

import (
	"testing"

	"github.com/travisennis/cake-repl/internal/cake"
)

func success(sessionID string) cake.TaskComplete {
	return cake.TaskComplete{Subtype: "success", SessionID: sessionID, TaskID: "t-1"}
}

func failure(sessionID string) cake.TaskComplete {
	return cake.TaskComplete{Subtype: "error_during_execution", IsError: true, SessionID: sessionID, Error: "boom"}
}

func TestFreshThenSuccessResumesSameSession(t *testing.T) {
	var s sessionState
	if mode, _ := s.RunOptions(); mode != cake.RunFresh {
		t.Fatalf("initial mode = %v, want fresh", mode)
	}
	s.OnTaskStart(cake.TaskStart{SessionID: "s-1", TaskID: "t-1"})
	s.OnTaskComplete(success("s-1"))

	mode, id := s.RunOptions()
	if mode != cake.RunResume || id != "s-1" {
		t.Errorf("after success mode=%v id=%q, want resume pinned to s-1", mode, id)
	}
	if s.SessionID != "s-1" {
		t.Errorf("session id = %q", s.SessionID)
	}
}

func TestResumeThenSuccessStaysPinned(t *testing.T) {
	var s sessionState
	s.UseResume("11111111-2222-3333-4444-555555555555")

	mode, id := s.RunOptions()
	if mode != cake.RunResume || id != "11111111-2222-3333-4444-555555555555" {
		t.Fatalf("mode=%v id=%q", mode, id)
	}

	s.OnTaskComplete(success("11111111-2222-3333-4444-555555555555"))
	mode, id = s.RunOptions()
	if mode != cake.RunResume || id != "11111111-2222-3333-4444-555555555555" {
		t.Errorf("after success mode=%v id=%q, want resume pinned to same session", mode, id)
	}
}

func TestSuccessWithoutSessionIDLeavesModeUnchanged(t *testing.T) {
	var s sessionState
	s.OnTaskComplete(success(""))

	mode, id := s.RunOptions()
	if mode != cake.RunFresh || id != "" {
		t.Errorf("after success with no session id mode=%v id=%q, want fresh with empty id", mode, id)
	}
}

func TestNewResetsToFresh(t *testing.T) {
	var s sessionState
	s.OnTaskStart(cake.TaskStart{SessionID: "s-1"})
	s.OnTaskComplete(success("s-1"))
	s.Reset()

	if mode, _ := s.RunOptions(); mode != cake.RunFresh {
		t.Errorf("mode after reset = %v, want fresh", mode)
	}
	if s.SessionID != "" || s.AnnouncedID != "" || s.LastComplete != nil {
		t.Errorf("reset should clear state: %+v", s)
	}
}

func TestFailureWithSessionIDPinsToResume(t *testing.T) {
	var s sessionState
	s.OnTaskStart(cake.TaskStart{SessionID: "s-1", TaskID: "t-1"})
	s.OnTaskComplete(failure("s-1"))

	mode, id := s.RunOptions()
	if mode != cake.RunResume || id != "s-1" {
		t.Errorf("after failure mode=%v id=%q, want resume pinned to s-1", mode, id)
	}
	if s.LastComplete == nil || !s.LastComplete.IsError {
		t.Error("failure should still be recorded as last completion")
	}
}

// A task can fail before ever announcing a session id. There is nothing to
// resume, so the run mode must stay where it was.
func TestFailureWithoutSessionIDDoesNotAdvanceMode(t *testing.T) {
	var s sessionState
	s.OnTaskComplete(failure(""))
	if mode, id := s.RunOptions(); mode != cake.RunFresh || id != "" {
		t.Errorf("fresh + failure with no session id mode=%v id=%q, want fresh with empty id", mode, id)
	}

	s.UseResume("11111111-2222-3333-4444-555555555555")
	s.OnTaskComplete(failure(""))
	mode, id := s.RunOptions()
	if mode != cake.RunResume || id != "11111111-2222-3333-4444-555555555555" {
		t.Errorf("resume + failure with no session id mode=%v id=%q, want resume unchanged", mode, id)
	}
}

// A failure reporting a session id that differs from an explicitly requested
// one pins to the session cake actually ran, matching the success path.
func TestFailureAfterExplicitResumePinsToReportedSession(t *testing.T) {
	var s sessionState
	s.UseResume("11111111-2222-3333-4444-555555555555")
	s.OnTaskComplete(failure("s-1"))

	mode, id := s.RunOptions()
	if mode != cake.RunResume || id != "s-1" {
		t.Errorf("after failure mode=%v id=%q, want resume pinned to s-1", mode, id)
	}
}

func TestRunEndedAfterTaskStartPinsToSession(t *testing.T) {
	var s sessionState
	s.OnTaskStart(cake.TaskStart{SessionID: "d8fceb36", TaskID: "t-1"})
	s.OnRunEnded()

	mode, id := s.RunOptions()
	if mode != cake.RunResume || id != "d8fceb36" {
		t.Errorf("after run end mode=%v id=%q, want resume pinned to d8fceb36", mode, id)
	}
}

func TestRunEndedBeforeTaskStartDoesNotInventSession(t *testing.T) {
	var s sessionState
	s.OnRunEnded()

	mode, id := s.RunOptions()
	if mode != cake.RunFresh || id != "" {
		t.Errorf("fresh + run end before TaskStart mode=%v id=%q, want fresh with empty id", mode, id)
	}
}

func TestRunEndedDuringExplicitResumePreservesResumeID(t *testing.T) {
	var s sessionState
	s.UseResume("11111111-2222-3333-4444-555555555555")
	s.OnTaskStart(cake.TaskStart{SessionID: "11111111-2222-3333-4444-555555555555", TaskID: "t-1"})
	s.OnRunEnded()

	mode, id := s.RunOptions()
	if mode != cake.RunResume || id != "11111111-2222-3333-4444-555555555555" {
		t.Errorf("after run end mode=%v id=%q, want resume pinned to explicit session", mode, id)
	}
}

// A completed session A leaves its id behind as history. Retargeting to B and
// then ending the new run before it announces its own id must not pin back to
// A: only the current invocation's announced id may pin.
func TestRunEndedAfterRetargetDoesNotRestorePreviousSession(t *testing.T) {
	var s sessionState
	s.OnTaskStart(cake.TaskStart{SessionID: "aaaaaaaa-1111-2222-3333-444444444444"})
	s.OnTaskComplete(success("aaaaaaaa-1111-2222-3333-444444444444"))

	s.UseResume("bbbbbbbb-1111-2222-3333-444444444444")
	s.OnRunEnded()

	mode, id := s.RunOptions()
	if mode != cake.RunResume || id != "bbbbbbbb-1111-2222-3333-444444444444" {
		t.Errorf("after retarget + run end mode=%v id=%q, want resume pinned to the new target", mode, id)
	}
}

// Ending a run that announced its own id re-pins the announced session even
// when a different target was requested, matching the completion path.
func TestRunEndedAfterRetargetPinsAnnouncedSession(t *testing.T) {
	var s sessionState
	s.UseResume("bbbbbbbb-1111-2222-3333-444444444444")
	s.OnTaskStart(cake.TaskStart{SessionID: "aaaaaaaa-1111-2222-3333-444444444444"})
	s.OnRunEnded()

	mode, id := s.RunOptions()
	if mode != cake.RunResume || id != "aaaaaaaa-1111-2222-3333-444444444444" {
		t.Errorf("after run end mode=%v id=%q, want resume pinned to the announced session", mode, id)
	}
}

// A retarget drops the identity of the session being left. Nothing the REPL
// displays, copies, or prints on exit may present session A as the session the
// next prompt continues. The last completion stays recorded because /session
// labels it as history.
func TestUseResumeRetargetDropsPreviousSessionIdentity(t *testing.T) {
	var s sessionState
	s.OnTaskStart(cake.TaskStart{SessionID: "aaaaaaaa-1111-2222-3333-444444444444", TaskID: "t-a"})
	s.OnModel("zen", "glm-5.1")
	s.OnTaskComplete(success("aaaaaaaa-1111-2222-3333-444444444444"))

	s.UseResume("bbbbbbbb-1111-2222-3333-444444444444")

	if s.SessionID != "" || s.TaskID != "" || s.AnnouncedID != "" || s.Model != "" {
		t.Errorf("retarget kept previous identity: %+v", s)
	}
	if mode, id := s.RunOptions(); mode != cake.RunResume || id != "bbbbbbbb-1111-2222-3333-444444444444" {
		t.Errorf("after retarget mode=%v id=%q, want resume pinned to the new target", mode, id)
	}
	if s.LastComplete == nil {
		t.Error("retarget dropped the last completion, which is displayed as history")
	}
}

// Re-selecting the session already targeted is not a retarget: the reported
// identity and last completion stay, so /session keeps describing it.
func TestUseResumeSameTargetKeepsReportedIdentity(t *testing.T) {
	id := "aaaaaaaa-1111-2222-3333-444444444444"
	var s sessionState
	s.OnTaskStart(cake.TaskStart{SessionID: id, TaskID: "t-1"})
	s.OnModel("zen", "")
	s.OnTaskComplete(success(id))

	s.UseResume(id)

	if s.SessionID != id || s.TaskID != "t-1" || s.AnnouncedID != id || s.Model != "zen" {
		t.Errorf("same-target resume dropped identity: %+v", s)
	}
	if mode, resumeID := s.RunOptions(); mode != cake.RunResume || resumeID != id {
		t.Errorf("after same-target resume mode=%v id=%q, want resume %q", mode, resumeID, id)
	}
}

// A run that ends without a session id after a retarget must not put the
// session the REPL left back in charge, and must not invent one.
func TestIDlessCompletionAfterRetargetKeepsNewTarget(t *testing.T) {
	const newTarget = "bbbbbbbb-1111-2222-3333-444444444444"
	var s sessionState
	s.OnTaskStart(cake.TaskStart{SessionID: "aaaaaaaa-1111-2222-3333-444444444444", TaskID: "t-a"})
	s.OnTaskComplete(success("aaaaaaaa-1111-2222-3333-444444444444"))
	s.UseResume(newTarget)

	s.OnTaskComplete(success(""))

	if s.SessionID != "" {
		t.Errorf("session id = %q, want none restored", s.SessionID)
	}
	if mode, id := s.RunOptions(); mode != cake.RunResume || id != newTarget {
		t.Errorf("after id-less completion mode=%v id=%q, want resume pinned to %q", mode, id, newTarget)
	}
}

func TestOnModelPrefersModelsConfigEntryName(t *testing.T) {
	var s sessionState
	s.OnModel("zen", "glm-5.1")
	if s.Model != "zen" {
		t.Fatalf("model = %q, want the [[models]] entry name", s.Model)
	}

	s.OnModel("", "glm-5.1")
	if s.Model != "glm-5.1" {
		t.Errorf("model without an entry name = %q, want the provider model ID", s.Model)
	}

	s.OnModel("", "")
	if s.Model != "glm-5.1" {
		t.Errorf("identity-less event cleared model to %q", s.Model)
	}
}

func TestOnModelClearsOnNewSessionAndResumeSwitch(t *testing.T) {
	var s sessionState
	s.OnModel("zen", "")
	s.Reset()
	if s.Model != "" {
		t.Fatalf("reset left model = %q", s.Model)
	}

	s.OnModel("zen", "")
	s.UseResume("11111111-2222-3333-4444-555555555555")
	if s.Model != "" {
		t.Errorf("resume switch left stale model = %q", s.Model)
	}
}

func completeTurn(input int, window int) cake.TurnUsage {
	return cake.TurnUsage{
		Turn:          1,
		Usage:         cake.Usage{InputTokens: input},
		UsagePresence: usagePresenceComplete,
		ContextWindow: window,
	}
}

// The newest complete record wins; earlier measurements are replaced even when
// the later one is smaller (a compaction or a shorter prompt).
func TestOnTurnUsageKeepsLatestCompleteMeasurement(t *testing.T) {
	var s sessionState
	s.OnTurnUsage(completeTurn(132_000, 1_000_000))
	if s.Context == nil || s.Context.Tokens != 132_000 || s.Context.Window != 1_000_000 {
		t.Fatalf("first measurement = %+v", s.Context)
	}

	s.OnTurnUsage(completeTurn(40_000, 1_000_000))
	if s.Context == nil || s.Context.Tokens != 40_000 {
		t.Errorf("latest measurement = %+v, want 40000", s.Context)
	}
}

// Partial usage is not a context measurement and must not replace the last
// complete one, nor create a segment on its own.
func TestOnTurnUsageIgnoresPartialRecords(t *testing.T) {
	var s sessionState
	partial := completeTurn(9_000, 1_000_000)
	partial.UsagePresence = "partial"
	s.OnTurnUsage(partial)
	if s.Context != nil {
		t.Fatalf("partial-only measurement = %+v, want none", s.Context)
	}

	s.OnTurnUsage(completeTurn(8_000, 1_000_000))
	partial.Usage.InputTokens = 9_500
	s.OnTurnUsage(partial)
	if s.Context == nil || s.Context.Tokens != 8_000 {
		t.Errorf("partial record replaced the complete one: %+v", s.Context)
	}
}

// Older records omit usage_presence; an absent value is complete, not partial.
func TestOnTurnUsageTreatsMissingPresenceAsComplete(t *testing.T) {
	var s sessionState
	s.OnTurnUsage(cake.TurnUsage{Usage: cake.Usage{InputTokens: 42_000}})
	if s.Context == nil || s.Context.Tokens != 42_000 || s.Context.Window != 0 {
		t.Errorf("legacy measurement = %+v", s.Context)
	}
}

// A record without a positive input count is not a measurement: it cannot
// describe the context, and showing 0 would be worse than omitting the segment.
func TestOnTurnUsageIgnoresEmptyUsage(t *testing.T) {
	var s sessionState
	s.OnTurnUsage(cake.TurnUsage{UsagePresence: usagePresenceComplete, ContextWindow: 1_000_000})
	if s.Context != nil {
		t.Errorf("empty usage recorded as %+v", s.Context)
	}
}

// The window belongs to the measurement that carried it. A later complete
// record without a window means the model configures none, so the display
// degrades to a bare token count instead of pairing new tokens with an old
// model's window.
func TestOnTurnUsageLatestRecordDropsAbsentWindow(t *testing.T) {
	var s sessionState
	s.OnTurnUsage(completeTurn(10_000, 1_000_000))
	s.OnTurnUsage(completeTurn(11_000, 0))
	if s.Context == nil || s.Context.Tokens != 11_000 || s.Context.Window != 0 {
		t.Errorf("measurement = %+v, want 11000 with no window", s.Context)
	}
}

// A replayed checkpoint seeds the status estimate from its after_tokens while
// keeping the window the model last reported. It is marked estimated, and the
// next complete turn_usage replaces it with a real measurement.
func TestOnCompactionSeedsEstimatedContextFromKnownWindow(t *testing.T) {
	var s sessionState
	s.OnTurnUsage(completeTurn(118_000, 200_000))

	s.OnCompaction(cake.Compaction{EstimatedInput: cake.CompactionEstimate{BeforeTokens: 118_000, AfterTokens: 24_000}})
	if s.Context == nil || s.Context.Tokens != 24_000 || s.Context.Window != 200_000 || !s.Context.Estimated {
		t.Fatalf("context = %+v, want estimated 24000/200000", s.Context)
	}

	s.OnTurnUsage(completeTurn(6_000, 200_000))
	if s.Context == nil || s.Context.Tokens != 6_000 || s.Context.Estimated {
		t.Errorf("live measurement did not replace the estimate: %+v", s.Context)
	}
}

// Without a measurement before it, a checkpoint still seeds the estimate but
// renders no fabricated window.
func TestOnCompactionWithoutKnownWindowUsesBareEstimate(t *testing.T) {
	var s sessionState
	s.OnCompaction(cake.Compaction{EstimatedInput: cake.CompactionEstimate{AfterTokens: 24_000}})
	if s.Context == nil || s.Context.Tokens != 24_000 || s.Context.Window != 0 || !s.Context.Estimated {
		t.Errorf("context = %+v, want estimated 24000 with no window", s.Context)
	}
}

// A live compactor supplies the window it used; when its document omits one,
// the session keeps the window already known. A non-positive estimate leaves the
// current figure alone.
func TestOnCompactionEstimateUsesResultWindowAndKeepsKnownWindow(t *testing.T) {
	var s sessionState
	s.OnTurnUsage(completeTurn(170_000, 200_000))

	s.OnCompactionEstimate(24_000, 200_000)
	if s.Context == nil || s.Context.Tokens != 24_000 || s.Context.Window != 200_000 || !s.Context.Estimated {
		t.Fatalf("context = %+v, want estimated 24000/200000", s.Context)
	}

	s.OnCompactionEstimate(12_000, 0)
	if s.Context == nil || s.Context.Tokens != 12_000 || s.Context.Window != 200_000 || !s.Context.Estimated {
		t.Fatalf("context = %+v, want estimated 12000/200000 with the known window", s.Context)
	}

	s.OnCompactionEstimate(0, 100_000)
	if s.Context == nil || s.Context.Tokens != 12_000 || s.Context.Window != 200_000 {
		t.Fatalf("a zero estimate changed the context: %+v", s.Context)
	}
}

// A checkpoint with no usable estimate is not a measurement: it must not clear
// or replace one already known.
func TestOnCompactionIgnoresMissingEstimate(t *testing.T) {
	var s sessionState
	s.OnTurnUsage(completeTurn(10_000, 1_000_000))
	s.OnCompaction(cake.Compaction{EstimatedInput: cake.CompactionEstimate{BeforeTokens: 118_000}})
	if s.Context == nil || s.Context.Tokens != 10_000 || s.Context.Estimated {
		t.Errorf("context = %+v, want the unchanged measurement", s.Context)
	}
}

func TestContextUsageClearsOnNewSessionAndResumeSwitch(t *testing.T) {
	var s sessionState
	s.OnTurnUsage(completeTurn(10_000, 1_000_000))
	s.Reset()
	if s.Context != nil {
		t.Fatalf("reset left context = %+v", s.Context)
	}

	s.OnTurnUsage(completeTurn(10_000, 1_000_000))
	s.UseResume("11111111-2222-3333-4444-555555555555")
	if s.Context != nil {
		t.Errorf("resume switch left stale context = %+v", s.Context)
	}
}

// Re-selecting the current target keeps the session's own measurement, just
// as it keeps the reported identity.
func TestUseResumeSameTargetKeepsContextUsage(t *testing.T) {
	id := "aaaaaaaa-1111-2222-3333-444444444444"
	var s sessionState
	s.UseResume(id)
	s.OnTaskStart(cake.TaskStart{SessionID: id})
	s.OnTurnUsage(completeTurn(10_000, 1_000_000))

	s.UseResume(id)

	if s.Context == nil || s.Context.Tokens != 10_000 {
		t.Errorf("same-target resume dropped context: %+v", s.Context)
	}
}
