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

// OnTaskComplete records the outcome. Once a session id is known, future
// prompts are pinned to it via --resume <id>, so a newer session created by
// another cake process in the same cwd cannot hijack the conversation. This
// holds whether the task succeeded or failed: cake writes a resumable session
// file either way, and a failed run that is not pinned would be orphaned by
// the next prompt. Only a *successful* task with no session id falls back to
// --continue; on failure the run mode is left untouched rather than advanced
// into the mode that is itself the hijack vector.
func (s *sessionState) OnTaskComplete(e cake.TaskComplete) {
	s.LastComplete = &e
	if e.SessionID != "" {
		s.SessionID = e.SessionID
	}
	if e.TaskID != "" {
		s.TaskID = e.TaskID
	}

	if s.pinToSession() {
		return
	}
	if !e.IsError {
		s.NextMode = cake.RunContinue
		s.ResumeID = ""
	}
}

// OnCancel records that the current run was interrupted by the user. If a
// session id has already been announced, future prompts are pinned to that
// session so the next submission does not accidentally create a new one. This
// preserves the hijack-prevention boundary even when a task is cut short.
func (s *sessionState) OnCancel() {
	s.pinToSession()
}

// pinToSession points the next prompt at the known session id and reports
// whether it had one to pin to.
func (s *sessionState) pinToSession() bool {
	if s.SessionID == "" {
		return false
	}
	s.NextMode = cake.RunResume
	s.ResumeID = s.SessionID
	return true
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
