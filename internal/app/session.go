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
	// Model is the model identity cake reported for the current session: the
	// [[models]] entry name when known, else the provider model ID. Empty means
	// cake has not reported one, so the status line falls back to the
	// CLI/config value. It is cleared whenever the session target changes.
	Model string
	// AnnouncedID is the session id the current invocation reported through
	// task_start (or, failing that, its completion). Only an id an invocation
	// actually announced may pin the next prompt: a stale id left behind by an
	// earlier session must never retarget the run. It is cleared whenever the
	// session target changes, so it always belongs to the invocation being run.
	AnnouncedID string
}

// RunOptions returns the mode and resume id for the next cake invocation.
func (s *sessionState) RunOptions() (cake.RunMode, string) {
	return s.NextMode, s.ResumeID
}

// OnModel records the model identity cake reported for the session. The
// [[models]] entry name (model_config) is preferred because it stays resolvable
// when several entries share one provider ID; the provider model ID (model) is
// the fallback. An event carrying neither leaves the recorded value unchanged.
func (s *sessionState) OnModel(modelConfig, model string) {
	switch {
	case modelConfig != "":
		s.Model = modelConfig
	case model != "":
		s.Model = model
	}
}

// OnTaskStart records ids announced at the start of a task. The announced
// session id becomes the current invocation's identity, the only one eligible
// to pin the next prompt if the run ends without a completion record.
func (s *sessionState) OnTaskStart(e cake.TaskStart) {
	s.SessionID = e.SessionID
	s.TaskID = e.TaskID
	s.AnnouncedID = e.SessionID
}

// OnTaskComplete records the outcome. Once a session id is known, future
// prompts are pinned to it via --resume <id>, so a newer session created by
// another cake process in the same cwd cannot hijack the conversation. This
// holds whether the task succeeded or failed: cake writes a resumable session
// file either way, and a failed run that is not pinned would be orphaned by
// the next prompt. When no session id is reported, the current run mode is
// left unchanged.
func (s *sessionState) OnTaskComplete(e cake.TaskComplete) {
	s.LastComplete = &e
	if e.SessionID != "" {
		s.SessionID = e.SessionID
		s.AnnouncedID = e.SessionID
	}
	if e.TaskID != "" {
		s.TaskID = e.TaskID
	}

	s.pinToSession()
}

// OnRunEnded pins the next prompt to the session id the just-finished run
// announced. It covers every way a run can stop without delivering a
// completion record — cancellation, a nonzero exit, a wait error, or a clean
// EOF mid-task — so resumable work cake already wrote is continued instead of
// orphaned. A run that announced no id leaves the current run mode unchanged.
func (s *sessionState) OnRunEnded() {
	s.pinToSession()
}

// pinToSession points the next prompt at the id the current invocation
// announced. When it announced none, the current run mode remains unchanged.
func (s *sessionState) pinToSession() {
	if s.AnnouncedID == "" {
		return
	}
	s.NextMode = cake.RunResume
	s.ResumeID = s.AnnouncedID
}

// Reset clears all session state; the next prompt starts a fresh session.
func (s *sessionState) Reset() {
	*s = sessionState{NextMode: cake.RunFresh}
}

// UseResume makes the next prompt resume a specific session.
func (s *sessionState) UseResume(id string) {
	s.NextMode = cake.RunResume
	s.ResumeID = id
	// The previous invocation's announced id belongs to the old target; drop
	// it so ending a run before the new target announces its own id cannot
	// pin back to the session we just retargeted away from.
	s.AnnouncedID = ""
	// The displayed model belongs to the previous session; drop it so the
	// status line does not show a stale identity for the newly targeted one.
	s.Model = ""
}
