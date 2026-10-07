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
	if mode, _ := got.session.RunOptions(); mode != cake.RunResume {
		t.Errorf("run mode = %v, want RunResume", mode)
	}
	if it := lastItem(t, got); it.Kind != ui.KindInfo || !strings.Contains(it.Text, browserID2) {
		t.Errorf("notice = %+v, want an info item naming %s", it, browserID2)
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
