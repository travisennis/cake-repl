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

func TestFreshThenSuccessContinues(t *testing.T) {
	var s sessionState
	if mode, _ := s.RunOptions(); mode != cake.RunFresh {
		t.Fatalf("initial mode = %v, want fresh", mode)
	}
	s.OnTaskStart(cake.TaskStart{SessionID: "s-1", TaskID: "t-1"})
	s.OnTaskComplete(success("s-1"))

	mode, _ := s.RunOptions()
	if mode != cake.RunContinue {
		t.Errorf("mode after success = %v, want continue", mode)
	}
	if s.SessionID != "s-1" {
		t.Errorf("session id = %q", s.SessionID)
	}
}

func TestResumeThenSuccessContinues(t *testing.T) {
	var s sessionState
	s.UseResume("11111111-2222-3333-4444-555555555555")

	mode, id := s.RunOptions()
	if mode != cake.RunResume || id != "11111111-2222-3333-4444-555555555555" {
		t.Fatalf("mode=%v id=%q", mode, id)
	}

	s.OnTaskComplete(success("11111111-2222-3333-4444-555555555555"))
	mode, id = s.RunOptions()
	if mode != cake.RunContinue || id != "" {
		t.Errorf("after success mode=%v id=%q, want continue with empty id", mode, id)
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
	if s.SessionID != "" || s.LastComplete != nil {
		t.Errorf("reset should clear state: %+v", s)
	}
}

func TestFailureDoesNotAdvanceMode(t *testing.T) {
	var s sessionState
	s.OnTaskComplete(failure("s-1"))
	if mode, _ := s.RunOptions(); mode != cake.RunFresh {
		t.Errorf("fresh + failure should stay fresh, got %v", mode)
	}

	s.UseResume("11111111-2222-3333-4444-555555555555")
	s.OnTaskComplete(failure("s-1"))
	mode, id := s.RunOptions()
	if mode != cake.RunResume || id != "11111111-2222-3333-4444-555555555555" {
		t.Errorf("resume + failure should stay resume, got mode=%v id=%q", mode, id)
	}

	if s.LastComplete == nil || !s.LastComplete.IsError {
		t.Error("failure should still be recorded as last completion")
	}
}

func TestUseContinueClearsResumeID(t *testing.T) {
	var s sessionState
	s.UseResume("11111111-2222-3333-4444-555555555555")
	s.UseContinue()
	mode, id := s.RunOptions()
	if mode != cake.RunContinue || id != "" {
		t.Errorf("mode=%v id=%q", mode, id)
	}
}
