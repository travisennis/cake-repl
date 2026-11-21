package app

import "github.com/travisennis/cake-repl/internal/cake"

type sessionState struct {
	generation   sessionGeneration
	SessionID    string
	TaskID       string
	NextMode     cake.RunMode
	ResumeID     string
	LastComplete *cake.TaskComplete
	Model        string
	AnnouncedID  string
	Context      *ContextUsage
}

func newSessionState(next cake.RunMode, resumeID string) sessionState {
	if resumeID == "" {
		next = cake.RunFresh
	}
	if next != cake.RunResume {
		resumeID = ""
	}
	return sessionState{generation: 1, NextMode: next, ResumeID: resumeID}
}

func (s *sessionState) Generation() sessionGeneration {
	return s.generation
}

type ContextUsage struct {
	Tokens    int
	Window    int
	Estimated bool
}

const usagePresenceComplete = "complete"

func (s *sessionState) RunOptions() (cake.RunMode, string) {
	return s.NextMode, s.ResumeID
}

func (s *sessionState) OnModel(modelConfig, model string) {
	switch {
	case modelConfig != "":
		s.Model = modelConfig
	case model != "":
		s.Model = model
	}
}

func (s *sessionState) OnTaskStart(e cake.TaskStart) {
	s.announce(e.SessionID)
	s.TaskID = e.TaskID
}

func (s *sessionState) announce(id string) {
	s.SessionID = id
	s.AnnouncedID = id
}

func (s *sessionState) OnTurnUsage(e cake.TurnUsage) {
	if e.UsagePresence != "" && e.UsagePresence != usagePresenceComplete {
		return
	}
	if e.Usage.InputTokens <= 0 {
		return
	}
	s.Context = &ContextUsage{Tokens: e.Usage.InputTokens, Window: e.ContextWindow}
}

func (s *sessionState) OnCompaction(e cake.Compaction) {
	s.OnCompactionEstimate(e.EstimatedInput.AfterTokens, 0)
}

func (s *sessionState) OnCompactionEstimate(after, window int) {
	if after <= 0 {
		return
	}
	if window <= 0 && s.Context != nil {
		window = s.Context.Window
	}
	s.Context = &ContextUsage{Tokens: after, Window: window, Estimated: true}
}

func (s *sessionState) OnTaskComplete(e cake.TaskComplete) {
	s.LastComplete = &e
	if e.SessionID != "" {
		s.announce(e.SessionID)
	}
	if e.TaskID != "" {
		s.TaskID = e.TaskID
	}

	s.pinToSession()
}

func (s *sessionState) OnRunEnded() {
	s.pinToSession()
}

func (s *sessionState) pinToSession() {
	if s.AnnouncedID == "" {
		return
	}
	s.NextMode = cake.RunResume
	s.ResumeID = s.AnnouncedID
}

func (s *sessionState) Reset() {
	generation := s.generation + 1
	*s = sessionState{generation: generation, NextMode: cake.RunFresh}
}

func (s *sessionState) UseResume(id string) {
	if id == "" {
		return
	}
	if s.NextMode != cake.RunResume || s.ResumeID != id {
		s.generation++
		s.SessionID = ""
		s.TaskID = ""
		s.AnnouncedID = ""
		s.Model = ""
		s.Context = nil
	}
	s.NextMode = cake.RunResume
	s.ResumeID = id
}
