package app

// activity is the operation a presentation surface names. Idle is represented
// separately from the composer's "ready" label because the status line has
// always called that state "idle".
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

// presentation is the one derivation of what the REPL is doing. The title,
// status line, composer activity label, composer hint, and view body all read
// this value, so a state cannot name one operation on one surface and advertise
// another operation's action on the next.
type presentation struct {
	activity    activity
	label       string
	hint        string
	titleMarker string
	browser     bool
}

// activityLabel is the composer's label. The status line uses label so idle is
// named "idle" there and "ready" in the composer, preserving both existing
// surfaces.
func (p presentation) activityLabel() string {
	if p.activity == activityReady {
		return "ready"
	}
	return p.label
}

// presentationState derives every presentation surface from the typed lifecycle
// states. Its precedence follows the admission gates: exit, browser, run,
// replay, compaction, then the idle composer. Suggestion text is presentation,
// not a request phase, so it only changes the idle hint.
func (m Model) presentationState() presentation {
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
	case m.suggestionActive() && m.input.Value() == "":
		return presentation{
			activity: activityReady,
			label:    "idle",
			hint:     "Ctrl+S use suggestion · type to dismiss",
		}
	default:
		return presentation{
			activity: activityReady,
			label:    "idle",
			hint:     "Ctrl+S submit · Enter newline · /help",
		}
	}
}

// compactionStateLabel names the in-flight compactor and its whole-second age.
// The status line and composer share it so the two surfaces cannot disagree.
func (m Model) compactionStateLabel() string {
	return "compacting " + compactionElapsedText(m.compactionElapsed)
}
