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
