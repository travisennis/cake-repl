package app

type activity uint8

const (
	activityReady activity = iota
	activityRunning
	activityCompacting
	activityStoppingTask
	activityStoppingCompaction
	activityLoadingHistory
	activityStoppingHistory
	activityBrowsing
	activityExiting
)

type presentation struct {
	activity    activity
	label       string
	hint        string
	titleMarker string
	browser     bool
	background  string
}

func (p presentation) activityLabel() string {
	if p.activity == activityReady {
		return "ready"
	}
	return p.label
}

func (m Model) presentationState() presentation {
	p := m.basePresentation()
	if m.suggestionInFlight() {
		p.background = "suggesting " + elapsedText(m.suggestion.elapsed)
	}
	return p
}

func (m Model) basePresentation() presentation {
	if m.exiting() {
		return presentation{
			activity:    activityExiting,
			label:       "exiting",
			hint:        "waiting for child processes to exit…",
			titleMarker: "[exiting]",
			browser:     m.browser.open(),
		}
	}
	if m.browser.open() {
		hint := "esc close"
		if m.browserSelectable() {
			hint = "↑/↓ move · enter resume · esc close"
		}
		return presentation{
			activity:    activityBrowsing,
			label:       "browsing sessions",
			hint:        hint,
			titleMarker: "[browsing]",
			browser:     true,
		}
	}
	switch {
	case m.runActive():
		return presentation{
			activity:    activityRunning,
			label:       "running",
			hint:        "Ctrl+C cancel · Ctrl+N new session",
			titleMarker: "[working]",
		}
	case m.runOp.draining():
		return presentation{
			activity:    activityStoppingTask,
			label:       "stopping task",
			hint:        "Ctrl+N new session",
			titleMarker: "[stopping]",
		}
	case m.replay.phase() == replayLoading:
		return presentation{
			activity:    activityLoadingHistory,
			label:       "loading history",
			hint:        "Ctrl+C exit · Ctrl+N new session",
			titleMarker: "[loading]",
		}
	case m.replay.phase() == replayDraining:
		return presentation{
			activity:    activityStoppingHistory,
			label:       "stopping history",
			hint:        "Ctrl+C exit · Ctrl+N new session",
			titleMarker: "[stopping]",
		}
	case m.compaction.active():
		return presentation{
			activity:    activityCompacting,
			label:       m.compactionStateLabel(),
			hint:        "Ctrl+C cancel · Ctrl+N new session",
			titleMarker: "[compacting]",
		}
	case m.compaction.draining():
		return presentation{
			activity:    activityStoppingCompaction,
			label:       "stopping compaction",
			hint:        "Ctrl+N new session",
			titleMarker: "[stopping]",
		}
	case m.suggestionActive():
		return presentation{
			activity: activityReady,
			label:    "idle",
			hint:     "Ctrl+S insert suggestion · type to hide",
		}
	default:
		return presentation{
			activity: activityReady,
			label:    "idle",
			hint:     "Ctrl+S submit · Enter newline · /help",
		}
	}
}

func (m Model) compactionStateLabel() string {
	return "compacting " + elapsedText(m.compactionElapsed)
}
