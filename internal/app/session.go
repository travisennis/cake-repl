package app

import "github.com/travisennis/cake-repl/internal/cake"

// sessionState tracks which cake session the next prompt should target. It is
// a pure state machine so run-mode transitions stay testable.
type sessionState struct {
	SessionID    string
	TaskID       string
	NextMode     cake.RunMode
	ResumeID     string
	LastComplete *cake.TaskComplete
}

// RunOptions returns the mode and resume id for the next cake invocation.
func (s *sessionState) RunOptions() (cake.RunMode, string) {
	return s.NextMode, s.ResumeID
}

// OnTaskStart records ids announced at the start of a task.
func (s *sessionState) OnTaskStart(e cake.TaskStart) {
	s.SessionID = e.SessionID
	s.TaskID = e.TaskID
}

// OnTaskComplete records the outcome. A successful task pins future prompts
// to the same session via --resume <id>, so a newer session created by
// another cake process in the same cwd cannot hijack the conversation. If no
// session id was ever reported, it falls back to --continue. A failed task
// does not advance the run mode.
func (s *sessionState) OnTaskComplete(e cake.TaskComplete) {
	s.LastComplete = &e
	if e.SessionID != "" {
		s.SessionID = e.SessionID
	}
	if e.TaskID != "" {
		s.TaskID = e.TaskID
	}
	if !e.IsError {
		if s.SessionID != "" {
			s.NextMode = cake.RunResume
			s.ResumeID = s.SessionID
		} else {
			s.NextMode = cake.RunContinue
			s.ResumeID = ""
		}
	}
}

// Reset clears all session state; the next prompt starts a fresh session.
func (s *sessionState) Reset() {
	*s = sessionState{NextMode: cake.RunFresh}
}

// UseContinue makes the next prompt continue cake's latest session.
func (s *sessionState) UseContinue() {
	s.NextMode = cake.RunContinue
	s.ResumeID = ""
}

// UseResume makes the next prompt resume a specific session.
func (s *sessionState) UseResume(id string) {
	s.NextMode = cake.RunResume
	s.ResumeID = id
}
