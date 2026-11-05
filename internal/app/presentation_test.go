package app

import (
	"strings"
	"testing"
	"time"

	"github.com/travisennis/cake-repl/internal/cake"
)

// presentationModel is the idle baseline for the presentation table. Cwd is
// fixed so the terminal-title expectation is explicit.
func presentationModel(t *testing.T) Model {
	t.Helper()
	m := newLaidOutModel()
	m.cfg.Cwd = "/tmp/project"
	m.cfg.Suggestions = SuggestionConfig{Enabled: true, Bin: "cake-suggest"}
	return m
}

// TestPresentationStateCoversReachableStates is the task's one table over the
// states the presentation derivation can actually reach. Each row asserts the
// title marker, status/composer labels, hint, browser selection, and the rendered
// view all name the same operation. It deliberately omits running+compacting;
// task 138 owns that mutual-exclusion invariant.
func TestPresentationStateCoversReachableStates(t *testing.T) {
	const (
		submitHint          = "Ctrl+S submit · Enter newline · /help"
		suggestionHint      = "Ctrl+S use suggestion · type to dismiss"
		cancelHint          = "Ctrl+C cancel · Ctrl+N new session"
		newSessionHint      = "Ctrl+N new session"
		replayHint          = "Ctrl+C exit · Ctrl+N new session"
		browserHint         = "↑/↓ move · enter resume · esc close"
		browserCloseHint    = "esc close"
		exitingHint         = "waiting for child processes to exit…"
		initialTimelineText = "cake-repl — /help for commands"
	)
	tests := []struct {
		name        string
		setup       func(*testing.T, *Model)
		activity    string
		label       string
		hint        string
		marker      string
		browser     bool
		browserBody string
	}{
		{
			name:     "idle",
			setup:    func(*testing.T, *Model) {},
			activity: "ready", label: "idle", hint: submitHint,
		},
		{
			name: "suggestion with empty input",
			setup: func(_ *testing.T, m *Model) {
				m.showSuggestion("Try the lifecycle test.")
			},
			activity: "ready", label: "idle", hint: suggestionHint,
		},
		{
			name: "suggestion with a non-empty draft",
			setup: func(_ *testing.T, m *Model) {
				m.showSuggestion("Try the lifecycle test.")
				m.input.SetValue("my own draft")
			},
			activity: "ready", label: "idle", hint: submitHint,
		},
		{
			name: "run in flight",
			setup: func(_ *testing.T, m *Model) {
				beginTestRun(m)
			},
			activity: "running", label: "running", hint: cancelHint, marker: "[working]",
		},
		{
			name: "run draining",
			setup: func(_ *testing.T, m *Model) {
				beginTestRun(m)
				m.runOp.cancel()
			},
			activity: "stopping task", label: "stopping task", hint: newSessionHint, marker: "[stopping]",
		},
		{
			name: "compaction in flight",
			setup: func(_ *testing.T, m *Model) {
				m.compaction.begin(operationKey{request: 1}, nil, make(chan struct{}))
				m.compactionElapsed = 5 * time.Second
			},
			activity: "compacting 5s", label: "compacting 5s", hint: cancelHint, marker: "[compacting]",
		},
		{
			name: "compaction draining",
			setup: func(_ *testing.T, m *Model) {
				m.compaction.begin(operationKey{request: 1}, nil, make(chan struct{}))
				m.compactionElapsed = 5 * time.Second
				m.compaction.cancel()
			},
			activity: "stopping compaction", label: "stopping compaction", hint: newSessionHint, marker: "[stopping]",
		},
		{
			name: "replay loading",
			setup: func(_ *testing.T, m *Model) {
				m.session = newSessionState(cake.RunResume, replayTestSessionID)
				beginTestReplay(m)
			},
			activity: "loading history", label: "loading history", hint: replayHint, marker: "[loading]",
		},
		{
			name: "replay draining",
			setup: func(_ *testing.T, m *Model) {
				m.session = newSessionState(cake.RunResume, replayTestSessionID)
				beginTestReplay(m)
				m.replay.cancel()
			},
			activity: "stopping history", label: "stopping history", hint: replayHint, marker: "[stopping]",
		},
		{
			name: "browser loading",
			setup: func(_ *testing.T, m *Model) {
				m.browserOpen = true
				m.browserLoading = true
			},
			activity: "browsing sessions", label: "browsing sessions", hint: browserCloseHint, marker: "[browsing]",
			browser: true, browserBody: "loading sessions…",
		},
		{
			name: "browser error",
			setup: func(_ *testing.T, m *Model) {
				m.browserOpen = true
				m.browserErr = "boom"
			},
			activity: "browsing sessions", label: "browsing sessions", hint: browserCloseHint, marker: "[browsing]",
			browser: true, browserBody: "sessions unavailable: boom",
		},
		{
			name: "browser empty",
			setup: func(_ *testing.T, m *Model) {
				m.browserOpen = true
			},
			activity: "browsing sessions", label: "browsing sessions", hint: browserCloseHint, marker: "[browsing]",
			browser: true, browserBody: "no sessions in project",
		},
		{
			name: "browser list",
			setup: func(t *testing.T, m *Model) {
				*m = browserModel(t, []cake.SessionSummary{{
					SessionID:   browserID1,
					FirstPrompt: "one",
				}})
				m.cfg.Cwd = "/tmp/project"
			},
			activity: "browsing sessions", label: "browsing sessions", hint: browserHint, marker: "[browsing]",
			browser: true, browserBody: "11111111",
		},
		{
			name: "exiting",
			setup: func(_ *testing.T, m *Model) {
				m.exitPhase = phaseDraining
			},
			activity: "exiting", label: "exiting", hint: exitingHint, marker: "[exiting]",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := presentationModel(t)
			tt.setup(t, &m)
			m.syncViewport()

			p := m.presentationState()
			if got := p.activityLabel(); got != tt.activity {
				t.Errorf("composer activity = %q, want %q", got, tt.activity)
			}
			if p.label != tt.label {
				t.Errorf("status label = %q, want %q", p.label, tt.label)
			}
			if p.hint != tt.hint {
				t.Errorf("hint = %q, want %q", p.hint, tt.hint)
			}
			if p.titleMarker != tt.marker {
				t.Errorf("title marker = %q, want %q", p.titleMarker, tt.marker)
			}
			if p.browser != tt.browser {
				t.Errorf("browser = %v, want %v", p.browser, tt.browser)
			}

			gotTitle, ok := titleSetBy(m.titleCmd())
			if !ok {
				t.Fatal("titleCmd did not set a terminal title")
			}
			if want := windowTitle(m.cfg.Cwd, tt.marker); gotTitle != want {
				t.Errorf("terminal title = %q, want %q", gotTitle, want)
			}
			if line := m.statusLineFor(p); !strings.Contains(line, tt.label) {
				t.Errorf("status line = %q, want it to contain %q", line, tt.label)
			}

			view := m.View()
			if !strings.Contains(view, "prompt · "+tt.activity) {
				t.Errorf("view missing composer activity %q:\n%s", tt.activity, view)
			}
			if !strings.Contains(view, tt.hint) {
				t.Errorf("view missing hint %q:\n%s", tt.hint, view)
			}
			if tt.browser {
				if !strings.Contains(view, tt.browserBody) {
					t.Errorf("view missing browser body %q:\n%s", tt.browserBody, view)
				}
				if strings.Contains(view, initialTimelineText) {
					t.Errorf("browser view leaked the timeline:\n%s", view)
				}
			} else if !strings.Contains(view, initialTimelineText) {
				t.Errorf("view missing the timeline:\n%s", view)
			}

			gate := m.admitPrompt()
			if gate.allowed && !strings.Contains(tt.hint, "Ctrl+S") {
				t.Errorf("hint %q omits the submit action the gate accepts", tt.hint)
			}
			if !gate.allowed && strings.Contains(tt.hint, "Ctrl+S") {
				t.Errorf("hint %q advertises submit while the gate refuses it", tt.hint)
			}
			if tt.browser && m.browserSelectable() != (tt.hint == browserHint) {
				t.Errorf("browser hint %q disagrees with selectable=%v", tt.hint, m.browserSelectable())
			}
		})
	}
}

// TestViewNotReadyStillShowsStarting keeps the pre-layout frame independent of
// the presentation derivation. The title still follows lifecycle state while
// the first WindowSizeMsg is pending.
func TestViewNotReadyStillShowsStarting(t *testing.T) {
	m := New(Config{
		Cwd:         "/tmp/project",
		InitialMode: cake.RunResume,
		ResumeID:    replayTestSessionID,
	})
	if got := m.View(); got != "starting cake-repl…" {
		t.Errorf("not-ready view = %q", got)
	}
	requireTitle(t, m.titleCmd(), windowTitle(m.cfg.Cwd, "[loading]"))
}
