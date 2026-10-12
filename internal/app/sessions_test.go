package app

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"github.com/travisennis/cake-repl/internal/cake"
	"github.com/travisennis/cake-repl/internal/ui"
)

const (
	browserID1 = "11111111-2222-3333-4444-555555555555"
	browserID2 = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
)

// browserModel returns a laid-out model with the /sessions browser open and one
// listing already applied.
func browserModel(t *testing.T, sessions []cake.SessionSummary) Model {
	t.Helper()
	m := newLaidOutModel()
	m.browserOpen = true
	tm, _ := m.Update(sessionsLoadedMsg{sessions: sessions})
	return tm.(Model)
}

func TestParseCommandSessions(t *testing.T) {
	cmd, ok, err := ParseCommand("/sessions")
	if !ok || err != nil {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if cmd.Kind != CmdSessions {
		t.Errorf("kind = %v, want CmdSessions", cmd.Kind)
	}
}

func TestSessionsCommandOpensBrowserAndLoads(t *testing.T) {
	bin := writeFakeCake(t, `
cat <<'EOF'
{"schema_version":1,"data":{"sessions":[
  {"session_id":"`+browserID1+`","first_prompt":"first task","timestamp":"2026-10-07T11:20:44Z"},
  {"session_id":"`+browserID2+`","first_prompt":"second task","timestamp":"2026-10-06T09:00:00Z"}
]}}
EOF
exit 0
`)
	m := New(Config{CakeBin: bin, Cwd: t.TempDir()})
	m.width, m.height = 80, 24
	m.layout()

	tm, cmd := m.execCommand(Command{Kind: CmdSessions})
	m = tm.(Model)
	if !m.browserOpen || !m.browserLoading {
		t.Fatalf("browser not opened/loading: open=%v loading=%v", m.browserOpen, m.browserLoading)
	}
	if cmd == nil {
		t.Fatal("expected a load command")
	}
	if want := "sessions in " + filepath.Base(m.cfg.Cwd); m.sessionList.Title != want {
		t.Errorf("title = %q, want %q", m.sessionList.Title, want)
	}

	tm, _ = m.Update(cmd())
	m = tm.(Model)
	if m.browserLoading {
		t.Error("browser still loading after the listing arrived")
	}
	if len(m.sessionList.Items()) != 2 {
		t.Fatalf("got %d items, want 2", len(m.sessionList.Items()))
	}
	if got := m.View(); !strings.Contains(got, "11111111") || !strings.Contains(got, "first task") {
		t.Errorf("list view missing the loaded session:\n%s", got)
	}
}

func TestSessionsLoadedErrorKeepsBrowserOpen(t *testing.T) {
	m := browserModel(t, nil)
	m.browserLoading = true

	tm, _ := m.Update(sessionsLoadedMsg{err: errors.New("cake exploded\x1b[2J")})
	got := tm.(Model)
	if got.browserLoading {
		t.Error("browser still loading after an error")
	}
	if !got.browserOpen {
		t.Error("error should leave the browser open")
	}
	if !strings.Contains(got.browserErr, "cake exploded") {
		t.Errorf("browserErr = %q", got.browserErr)
	}
	view := got.View()
	if !strings.Contains(view, "cake exploded") {
		t.Errorf("error body missing the message:\n%s", view)
	}
	// The error text carries cake's stderr, which is attacker-influenceable, so
	// it must be sanitized at this render boundary like any timeline item.
	if strings.Contains(view, "\x1b[") {
		t.Errorf("error body leaked an escape sequence:\n%q", view)
	}
}

func TestBrowserEnterPinsSelectedSession(t *testing.T) {
	m := browserModel(t, []cake.SessionSummary{
		{SessionID: browserID1, FirstPrompt: "one"},
		{SessionID: browserID2, FirstPrompt: "two"},
	})
	// The REPL is in session 1; Enter on row 2 is a retarget, so it drops the
	// identity of the session being left exactly like /resume does.
	m.session.OnTaskStart(cake.TaskStart{SessionID: browserID1, TaskID: "t-1"})
	m.session.OnTaskComplete(success(browserID1))

	tm, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyDown}) // select the second row
	m = tm.(Model)

	tm, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	got := tm.(Model)
	if got.browserOpen {
		t.Error("Enter should close the browser")
	}
	if got.session.ResumeID != browserID2 {
		t.Errorf("resume id = %q, want %q", got.session.ResumeID, browserID2)
	}
	if got.session.SessionID != "" {
		t.Errorf("session id = %q, want the left session's identity dropped", got.session.SessionID)
	}
	if mode, _ := got.session.RunOptions(); mode != cake.RunResume {
		t.Errorf("run mode = %v, want RunResume", mode)
	}
	if it := lastItem(t, got); it.Kind != ui.KindInfo || !strings.Contains(it.Text, browserID2) {
		t.Errorf("notice = %+v, want an info item naming %s", it, browserID2)
	}
}

// TestBrowserEnterIgnoredWhileReloading: after a successful visit is left
// behind, reopening the browser starts a fresh load. While that load is in
// flight the previous rows are gone, so Enter must not resume one of them.
func TestBrowserEnterIgnoredWhileReloading(t *testing.T) {
	m := browserModel(t, []cake.SessionSummary{{SessionID: browserID1, FirstPrompt: "one"}})
	m.session.UseResume(browserID2)
	beforeMode, beforeID := m.session.RunOptions()

	tm, cmd := m.execCommand(Command{Kind: CmdSessions})
	m = tm.(Model)
	if !m.browserOpen || !m.browserLoading {
		t.Fatalf("reopen: open=%v loading=%v", m.browserOpen, m.browserLoading)
	}
	if cmd == nil {
		t.Fatal("expected a reload command")
	}
	if items := m.sessionList.Items(); len(items) != 0 {
		t.Fatalf("stale rows survive the reload: %d items", len(items))
	}
	// The loading body replaces the list, so the old row is not on the screen.
	if view := m.View(); !strings.Contains(view, "loading sessions") || strings.Contains(view, "11111111") {
		t.Errorf("reload view should show loading only:\n%s", view)
	}

	tm, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	got := tm.(Model)
	if !got.browserOpen {
		t.Error("Enter during a reload must leave the browser open")
	}
	if mode, id := got.session.RunOptions(); mode != beforeMode || id != beforeID {
		t.Errorf("run options changed: %v %q -> %v %q", beforeMode, beforeID, mode, id)
	}
	if got.session.ResumeID != browserID2 {
		t.Errorf("resume id = %q, want the untouched %q", got.session.ResumeID, browserID2)
	}
}

// TestBrowserEnterIgnoredAfterFailedReload: a failed reload must not leave the
// previous listing selectable behind the error body.
func TestBrowserEnterIgnoredAfterFailedReload(t *testing.T) {
	m := browserModel(t, []cake.SessionSummary{{SessionID: browserID1, FirstPrompt: "one"}})
	m.session.UseResume(browserID2)
	beforeMode, beforeID := m.session.RunOptions()

	tm, _ := m.execCommand(Command{Kind: CmdSessions})
	m = tm.(Model)
	tm, _ = m.Update(sessionsLoadedMsg{err: errors.New("cake exploded")})
	m = tm.(Model)
	if items := m.sessionList.Items(); len(items) != 0 {
		t.Fatalf("a failed reload left %d selectable rows", len(items))
	}
	if !strings.Contains(m.View(), "cake exploded") {
		t.Errorf("error body missing the message:\n%s", m.View())
	}

	tm, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	got := tm.(Model)
	if !got.browserOpen {
		t.Error("Enter after a failed reload must leave the browser open")
	}
	if mode, id := got.session.RunOptions(); mode != beforeMode || id != beforeID {
		t.Errorf("run options changed: %v %q -> %v %q", beforeMode, beforeID, mode, id)
	}
}

// TestBrowserEnterIgnoredOnEmptyListing: a successful listing with no sessions
// gives Enter nothing to resume.
func TestBrowserEnterIgnoredOnEmptyListing(t *testing.T) {
	m := browserModel(t, nil)
	m.session.UseResume(browserID2)
	beforeMode, beforeID := m.session.RunOptions()

	tm, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	got := tm.(Model)
	if !got.browserOpen {
		t.Error("Enter on an empty listing must leave the browser open")
	}
	if mode, id := got.session.RunOptions(); mode != beforeMode || id != beforeID {
		t.Errorf("run options changed: %v %q -> %v %q", beforeMode, beforeID, mode, id)
	}
}

// TestBrowserEnterSelectsOnlyTheCurrentListing: once the reload lands, only a
// row from the new listing is selectable, and Enter leaves the composer alone.
func TestBrowserEnterSelectsOnlyTheCurrentListing(t *testing.T) {
	m := browserModel(t, []cake.SessionSummary{{SessionID: browserID1, FirstPrompt: "one"}})

	tm, _ := m.execCommand(Command{Kind: CmdSessions})
	m = tm.(Model)
	m.input.SetValue("draft")
	tm, _ = m.Update(sessionsLoadedMsg{sessions: []cake.SessionSummary{{SessionID: browserID2, FirstPrompt: "two"}}})
	m = tm.(Model)
	if m.sessionList.Index() != 0 {
		t.Fatalf("cursor = %d, want the first row of the new listing", m.sessionList.Index())
	}

	tm, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	got := tm.(Model)
	if got.browserOpen {
		t.Error("Enter should close the browser")
	}
	if got.session.ResumeID != browserID2 {
		t.Errorf("resume id = %q, want %q", got.session.ResumeID, browserID2)
	}
	if got.input.Value() != "draft" {
		t.Errorf("browser Enter must not touch the composer: %q", got.input.Value())
	}
}

func TestBrowserCloseKeysLeaveNoState(t *testing.T) {
	for _, k := range []tea.KeyMsg{
		{Type: tea.KeyEsc},
		{Type: tea.KeyRunes, Runes: []rune{'q'}},
		{Type: tea.KeyCtrlC},
	} {
		m := browserModel(t, []cake.SessionSummary{{SessionID: browserID1, FirstPrompt: "one"}})
		before := len(m.items)
		tm, _ := m.handleKey(k)
		got := tm.(Model)
		if got.browserOpen {
			t.Errorf("%v should close the browser", k)
		}
		if got.session.ResumeID != "" {
			t.Errorf("%v should not pin a session, got %q", k, got.session.ResumeID)
		}
		if len(got.items) != before {
			t.Errorf("%v appended a timeline item", k)
		}
	}
}

func TestBrowserNavigationMovesCursor(t *testing.T) {
	m := browserModel(t, []cake.SessionSummary{
		{SessionID: browserID1, FirstPrompt: "one"},
		{SessionID: browserID2, FirstPrompt: "two"},
	})
	if m.sessionList.Index() != 0 {
		t.Fatalf("initial cursor = %d, want 0", m.sessionList.Index())
	}

	tm, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyDown})
	if got := tm.(Model).sessionList.Index(); got != 1 {
		t.Errorf("after down, cursor = %d, want 1", got)
	}
	tm, _ = tm.(Model).handleKey(tea.KeyMsg{Type: tea.KeyUp})
	if got := tm.(Model).sessionList.Index(); got != 0 {
		t.Errorf("after up, cursor = %d, want 0", got)
	}
}

func TestBrowserEmptyState(t *testing.T) {
	m := browserModel(t, nil)
	m.cfg.Cwd = "/tmp/myrepo"
	got := m.View()
	if !strings.Contains(got, "no sessions in myrepo") {
		t.Errorf("empty state missing:\n%s", got)
	}
}

func TestBrowserLoadingState(t *testing.T) {
	m := newLaidOutModel()
	m.browserOpen = true
	m.browserLoading = true
	if got := m.View(); !strings.Contains(got, "loading sessions") {
		t.Errorf("loading state missing:\n%s", got)
	}
}

// TestBrowserViewAsciiNoANSI checks that -no-color (the Ascii profile) renders
// the browser without escape sequences.
func TestBrowserViewAsciiNoANSI(t *testing.T) {
	orig := lipgloss.ColorProfile()
	defer lipgloss.SetColorProfile(orig)
	lipgloss.SetColorProfile(termenv.Ascii)

	m := browserModel(t, []cake.SessionSummary{{SessionID: browserID1, FirstPrompt: "one"}})
	if got := m.View(); strings.Contains(got, "\x1b[") {
		t.Errorf("browser view contains ANSI escapes in Ascii mode:\n%q", got)
	}
}

// TestBrowserViewStaysWithinTimelineRegion checks the fixed-chrome promise at a
// range of sizes and states: every browser state — loading, error, empty, list
// — occupies exactly the timeline's rows so the composer and status line never
// move, and no row is wider than the terminal. The error body is arbitrary,
// multiline stderr, and the key hint must be bounded to the width too.
func TestBrowserViewStaysWithinTimelineRegion(t *testing.T) {
	longErr := strings.Repeat("cake: a short stderr line\n", 100)
	sessions := []cake.SessionSummary{
		{SessionID: browserID1, FirstPrompt: "first task"},
		{SessionID: browserID2, FirstPrompt: "second task"},
	}
	cases := []struct {
		name   string
		width  int
		height int
		setup  func(*Model)
	}{
		{"loading", 80, 24, func(m *Model) { m.browserOpen, m.browserLoading = true, true }},
		{"list", 80, 24, func(m *Model) {
			m.browserOpen = true
			tm, _ := m.Update(sessionsLoadedMsg{sessions: sessions})
			*m = tm.(Model)
		}},
		{"empty", 80, 24, func(m *Model) { m.browserOpen = true }},
		{"error", 80, 24, func(m *Model) { m.browserOpen, m.browserErr = true, longErr }},
		{"narrow error", 20, 24, func(m *Model) { m.browserOpen, m.browserErr = true, longErr }},
		{"narrow list", 20, 24, func(m *Model) {
			m.browserOpen = true
			tm, _ := m.Update(sessionsLoadedMsg{sessions: sessions})
			*m = tm.(Model)
		}},
		{"height 1 error", 20, 7, func(m *Model) { m.browserOpen, m.browserErr = true, longErr }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := laidOut(tc.width, tc.height)
			tc.setup(&m)
			view := m.View()
			lines := strings.Split(view, "\n")
			// The whole frame is the timeline region plus the composer and the
			// one-row status line; a browser state that overflows pushes them off.
			if len(lines) != m.height {
				t.Fatalf("view height = %d, want %d: the composer/status line moved\n%s", len(lines), m.height, view)
			}
			region := lines[:m.timeline.Height]
			for i, line := range region {
				if w := lipgloss.Width(line); w > m.width {
					t.Errorf("region row %d width = %d, want <= %d: %q", i, w, m.width, line)
				}
			}
		})
	}
}

func TestSessionsRejectedWhileRunning(t *testing.T) {
	m := newLaidOutModel()
	m.running = true

	tm, _ := m.execCommand(Command{Kind: CmdSessions})
	got := tm.(Model)
	if got.browserOpen {
		t.Error("the browser must not open while a task is running")
	}
	if it := lastItem(t, got); it.Kind != ui.KindWarning {
		t.Errorf("expected a warning, got %+v", it)
	}
}

func TestSessionsRejectedWhileHydrating(t *testing.T) {
	m := newLaidOutModel()
	m.hydrating = true

	tm, _ := m.execCommand(Command{Kind: CmdSessions})
	got := tm.(Model)
	if got.browserOpen {
		t.Error("the browser must not open while history is loading")
	}
	if it := lastItem(t, got); it.Kind != ui.KindWarning {
		t.Errorf("expected a warning, got %+v", it)
	}
}

func TestNewSessionItemSanitizesAndFlattens(t *testing.T) {
	now := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	item := newSessionItem(cake.SessionSummary{
		SessionID:   browserID1,
		FirstPrompt: "line one\nline two\x1b[2J",
		Timestamp:   now.Add(-3 * time.Minute),
	}, now)

	if strings.ContainsAny(item.Title(), "\n\x1b") {
		t.Errorf("title must be single-line and escape-free: %q", item.Title())
	}
	if !strings.HasPrefix(item.Title(), "11111111") {
		t.Errorf("title should lead with the short id: %q", item.Title())
	}
	if !strings.Contains(item.Title(), "3m ago") {
		t.Errorf("title should carry the relative age: %q", item.Title())
	}
	if !strings.Contains(item.Title(), "line one line two") {
		t.Errorf("title should carry the flattened prompt: %q", item.Title())
	}
	if item.FilterValue() != browserID1 || item.Description() != "" {
		t.Errorf("unexpected item metadata: %+v", item)
	}
}

func TestRelativeTime(t *testing.T) {
	now := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name string
		at   time.Time
		want string
	}{
		{"zero", time.Time{}, "—"},
		{"seconds", now.Add(-30 * time.Second), "just now"},
		{"minutes", now.Add(-5 * time.Minute), "5m ago"},
		{"hours", now.Add(-3 * time.Hour), "3h ago"},
		{"days", now.Add(-2 * 24 * time.Hour), "2d ago"},
		{"old", now.AddDate(0, 0, -90), "2026-07-09"},
		{"future", now.Add(time.Hour), "just now"},
	}
	for _, tt := range tests {
		if got := relativeTime(tt.at, now); got != tt.want {
			t.Errorf("%s: relativeTime = %q, want %q", tt.name, got, tt.want)
		}
	}
}

// TestBrowserItemsAreListItems is a compile-time-ish guard that the browser
// stores real list items the delegate can render.
func TestBrowserItemsAreListItems(t *testing.T) {
	m := browserModel(t, []cake.SessionSummary{{SessionID: browserID1, FirstPrompt: "one"}})
	items := m.sessionList.Items()
	if len(items) != 1 {
		t.Fatalf("got %d items, want 1", len(items))
	}
	if _, ok := items[0].(sessionItem); !ok {
		t.Errorf("item %T is not a sessionItem", items[0])
	}
}
