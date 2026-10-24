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
	// Context is the newest context figure for the current session: a complete
	// cake measurement, or a replayed checkpoint's estimate that no newer
	// complete measurement has replaced. Nil means none is available yet, or the
	// target changed, so the status line omits the segment rather than showing a
	// stale figure.
	Context *ContextUsage
}

// ContextUsage is one context figure: Tokens is the prompt size the model saw on
// the newest complete main-agent attempt, or a compaction estimate when
// Estimated is set, and Window is the model's configured context window (a
// non-positive value means none). Estimated marks a figure supplied by a
// compaction estimate instead of a provider measurement, so the status line can
// qualify it.
type ContextUsage struct {
	Tokens    int
	Window    int
	Estimated bool
}

// usagePresenceComplete is the only usage_presence value that marks a turn
// usage record as a full measurement. An absent value is a legacy complete
// record; any other value is a partial record, which is not a measurement.
const usagePresenceComplete = "complete"

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

// OnTurnUsage records the newest complete context measurement from a
// main-agent turn_usage record. Partial records are ignored because their
// token counts do not describe the whole context, and a record without a
// positive input count is not a measurement at all. The newest complete record
// wins, including its window: a later record that carries no context_window
// clears the window rather than pairing a new measurement with a window from
// another model. A complete record also replaces an estimated compaction
// figure, whether replay emitted it after the checkpoint or a live turn
// reported it.
func (s *sessionState) OnTurnUsage(e cake.TurnUsage) {
	if e.UsagePresence != "" && e.UsagePresence != usagePresenceComplete {
		return
	}
	if e.Usage.InputTokens <= 0 {
		return
	}
	s.Context = &ContextUsage{Tokens: e.Usage.InputTokens, Window: e.ContextWindow}
}

// OnCompaction seeds the context display from a replayed checkpoint's own token
// estimate. At its position in the replay the checkpoint is the newest
// statement about the session's context, so its after_tokens replaces an older
// turn_usage figure, while the window stays the one the model last reported.
// The record carries no window of its own; OnCompactionEstimate keeps the known
// one and marks the figure estimated.
func (s *sessionState) OnCompaction(e cake.Compaction) {
	s.OnCompactionEstimate(e.EstimatedInput.AfterTokens, 0)
}

// OnCompactionEstimate records the newest context estimate for the current
// session, marked Estimated so a resume never presents cake's estimate as a
// provider measurement. A live compactor supplies its own after_tokens and, when
// reported, the window it used; a replayed checkpoint supplies after_tokens only
// and keeps the window already known. A non-positive after count is not an
// estimate and leaves the current figure unchanged. A newer complete turn_usage,
// replayed after the checkpoint or reported by a live turn, replaces it.
func (s *sessionState) OnCompactionEstimate(after, window int) {
	if after <= 0 {
		return
	}
	if window <= 0 && s.Context != nil {
		window = s.Context.Window
	}
	s.Context = &ContextUsage{Tokens: after, Window: window, Estimated: true}
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

// UseResume makes the next prompt resume a specific session. Retargeting away
// from the current session drops the identity of the session being left, so no
// surface — the status line, /session, /session copy, or the exit hint — can
// present that session as the one the next prompt continues. Until the new
// target announces a session id of its own it is named only by NextMode and
// ResumeID. Re-selecting the current target is a no-op that keeps the reported
// identity. LastComplete is deliberately kept: /session labels it as the last
// result, so it reads as the history it is and never as the current identity.
func (s *sessionState) UseResume(id string) {
	if s.NextMode != cake.RunResume || s.ResumeID != id {
		s.SessionID = ""
		s.TaskID = ""
		// The previous invocation's announced id belongs to the old target; drop
		// it so ending a run before the new target announces its own id cannot
		// pin back to the session we just retargeted away from.
		s.AnnouncedID = ""
		// The displayed model belongs to the previous session; drop it so the
		// status line does not show a stale identity for the newly targeted one.
		s.Model = ""
		// The context measurement belongs to the previous session too, so the
		// status line shows nothing until the new target measures its own.
		s.Context = nil
	}
	s.NextMode = cake.RunResume
	s.ResumeID = id
}
