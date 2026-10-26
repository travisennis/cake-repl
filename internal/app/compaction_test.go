package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"github.com/travisennis/cake-repl/internal/cake"
	"github.com/travisennis/cake-repl/internal/ui"
)

// compactionStartIndex returns the live start notice's index, or -1 when the
// timeline carries none.
func compactionStartIndex(m Model) int {
	for i := len(m.items) - 1; i >= 0; i-- {
		if m.items[i].Kind == ui.KindInfo && strings.HasPrefix(m.items[i].Text, compactionStartPrefix) {
			return i
		}
	}
	return -1
}

const compactionSessionID = "11111111-2222-3333-4444-555555555555"

func TestShouldCompactAfter(t *testing.T) {
	ready := func(t *testing.T) Model {
		t.Helper()
		m := laidOut(160, 24)
		m.cfg.Compaction = CompactionConfig{Enabled: true, Bin: "cake-compact", ThresholdPercent: 80, KeepTokens: 20000}
		m.session.OnTaskStart(cake.TaskStart{SessionID: compactionSessionID, TaskID: "t-1"})
		m.session.OnTaskComplete(success(compactionSessionID))
		m.applyEvent(cake.TurnUsage{
			Usage:         cake.Usage{InputTokens: 170_000},
			UsagePresence: "complete",
			ContextWindow: 200_000,
		})
		m.sawComplete = true
		return m
	}
	clean := cake.Result{ExitCode: 0}

	tests := []struct {
		name  string
		setup func(*Model)
		res   cake.Result
		want  bool
	}{
		{"threshold crossed", func(*Model) {}, clean, true},
		{"below the trigger headroom", func(m *Model) {
			m.session.Context.Tokens = 149_000 // trigger is 75% of 200k
		}, clean, false},
		{"at the trigger headroom", func(m *Model) {
			m.session.Context.Tokens = 150_000
		}, clean, true},
		{"estimated figure is re-checked", func(m *Model) {
			m.session.Context.Estimated = true
		}, clean, true},
		{"no window", func(m *Model) { m.session.Context.Window = 0 }, clean, false},
		{"no measurement", func(m *Model) { m.session.Context = nil }, clean, false},
		{"no pinned session", func(m *Model) { m.session.Reset() }, clean, false},
		{"incomplete turn", func(m *Model) { m.sawComplete = false }, clean, false},
		{"errored completion", func(m *Model) { m.session.LastComplete.IsError = true }, clean, false},
		{"canceled run", func(*Model) {}, cake.Result{Canceled: true}, false},
		{"failed run", func(*Model) {}, cake.Result{ExitCode: 1}, false},
		{"wait error", func(*Model) {}, cake.Result{Err: os.ErrDeadlineExceeded}, false},
		{"disabled", func(m *Model) { m.cfg.Compaction.Enabled = false }, clean, false},
		{"no session", func(m *Model) { m.cfg.NoSession = true }, clean, false},
		{"invalid threshold", func(m *Model) { m.cfg.Compaction.ThresholdPercent = 0 }, clean, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := ready(t)
			tt.setup(&m)
			if got := m.shouldCompactAfter(tt.res); got != tt.want {
				t.Fatalf("shouldCompactAfter = %v, want %v (context %+v)", got, tt.want, m.session.Context)
			}
		})
	}
}

func TestShouldCompactAfterAbsolute(t *testing.T) {
	ready := func(tokens, window int) Model {
		m := laidOut(160, 24)
		m.cfg.Compaction = CompactionConfig{
			Enabled:          true,
			Bin:              "cake-compact",
			MaxContextTokens: 400_000,
			TriggerTokens:    350_000,
			ThresholdPercent: 10, // percent mode would fire below 100k; absolute mode must not.
			KeepTokens:       20_000,
		}
		m.session.OnTaskStart(cake.TaskStart{SessionID: compactionSessionID, TaskID: "t-1"})
		m.session.OnTaskComplete(success(compactionSessionID))
		m.applyEvent(cake.TurnUsage{
			Usage:         cake.Usage{InputTokens: tokens},
			UsagePresence: "complete",
			ContextWindow: window,
		})
		m.sawComplete = true
		return m
	}

	tests := []struct {
		name   string
		tokens int
		window int
		want   bool
	}{
		{"below the configured trigger", 349_999, 1_000_000, false},
		{"at the configured trigger", 350_000, 1_000_000, true},
		{"above the ceiling still fires", 500_000, 1_000_000, true},
		{"percent mode is ignored in absolute mode", 100_000, 1_000_000, false},
		{"window at the ceiling stays below the configured trigger", 349_999, 400_000, false},
		{"window at the ceiling fires at the configured trigger", 350_000, 400_000, true},
		{"window below the ceiling stays below the 75 percent cap", 149_999, 200_000, false},
		{"window below the ceiling fires at the 75 percent cap", 150_000, 200_000, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := ready(tt.tokens, tt.window)
			if got := m.shouldCompactAfter(cake.Result{ExitCode: 0}); got != tt.want {
				t.Fatalf("shouldCompactAfter = %v, want %v (context %+v)", got, tt.want, m.session.Context)
			}
		})
	}

	t.Run("incomplete absolute config disables the trigger", func(t *testing.T) {
		m := ready(900_000, 1_000_000)
		m.cfg.Compaction.TriggerTokens = 0
		if m.shouldCompactAfter(cake.Result{ExitCode: 0}) {
			t.Fatal("partial absolute config fell back to percent mode")
		}
	})
}

func TestResolveCompactionTrigger(t *testing.T) {
	m := laidOut(160, 24)
	m.cfg.Compaction = CompactionConfig{
		Enabled:          true,
		MaxContextTokens: 400_000,
		TriggerTokens:    350_000,
		ThresholdPercent: 80,
	}

	tests := []struct {
		name   string
		window int
		want   int
	}{
		{"large window uses the configured trigger", 1_000_000, 350_000},
		{"window at the ceiling uses the configured trigger", 400_000, 350_000},
		{"window just below the ceiling uses the cap", 399_999, 299_999},
		{"window well below the ceiling uses the cap", 200_000, 150_000},
		{"tiny window stays below itself", 1_000, 750},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			trigger, ok := m.resolveCompactionTrigger(tt.window)
			if !ok || !trigger.absolute {
				t.Fatalf("resolveCompactionTrigger(%d) = %+v, %v, want absolute", tt.window, trigger, ok)
			}
			if trigger.tokens != tt.want {
				t.Fatalf("trigger = %d, want %d", trigger.tokens, tt.want)
			}
			if trigger.tokens > tt.window {
				t.Fatalf("trigger %d exceeds window %d", trigger.tokens, tt.window)
			}
		})
	}

	t.Run("configured trigger below the window cap wins", func(t *testing.T) {
		m.cfg.Compaction.MaxContextTokens = 500_000
		m.cfg.Compaction.TriggerTokens = 350_000
		trigger, ok := m.resolveCompactionTrigger(480_000)
		if !ok || !trigger.absolute || trigger.tokens != 350_000 {
			t.Fatalf("trigger = %+v, %v, want 350000", trigger, ok)
		}
	})

	t.Run("percent fallback keeps the five-point headroom", func(t *testing.T) {
		m.cfg.Compaction.MaxContextTokens = 0
		m.cfg.Compaction.TriggerTokens = 0
		trigger, ok := m.resolveCompactionTrigger(200_000)
		if !ok || trigger.absolute || trigger.percent != 75 {
			t.Fatalf("percent fallback = %+v, %v, want 75%%", trigger, ok)
		}
	})
}

// compactionModel returns a model whose last turn completed with a context over
// the trigger, as finishRun sees it.
func compactionModel(t *testing.T, bin string) Model {
	t.Helper()
	return compactionModelWithConfig(t, CompactionConfig{
		Enabled:          true,
		Bin:              bin,
		ThresholdPercent: 80,
		KeepTokens:       20000,
	}, 170_000, 200_000)
}

// absoluteCompactionModel returns the same completed-turn model under an
// absolute 400k ceiling / 350k trigger.
func absoluteCompactionModel(t *testing.T, bin string, tokens, window int) Model {
	t.Helper()
	return compactionModelWithConfig(t, CompactionConfig{
		Enabled:          true,
		Bin:              bin,
		MaxContextTokens: 400_000,
		TriggerTokens:    350_000,
		ThresholdPercent: 80,
		KeepTokens:       20_000,
	}, tokens, window)
}

func compactionModelWithConfig(t *testing.T, cfg CompactionConfig, tokens, window int) Model {
	t.Helper()
	m := laidOut(160, 24)
	m.cfg.Compaction = cfg
	m.cfg.CakeBin = "/opt/bin/cake"
	m.cfg.Model = "test-model"
	m.cfg.Profile = "test-profile"
	m.session.OnTaskStart(cake.TaskStart{SessionID: compactionSessionID, TaskID: "t-1"})
	m.applyEvent(cake.TurnUsage{
		Usage:         cake.Usage{InputTokens: tokens},
		UsagePresence: "complete",
		ContextWindow: window,
	})
	m.applyEvent(cake.TaskComplete{Subtype: "success", SessionID: compactionSessionID, TaskID: "t-1"})
	m.running = true
	return m
}

// runCompactionCmd executes a finishRun command tree the way the Bubble Tea
// runtime does and returns a channel of the compactor result. Other messages
// (the title sequence, spinner ticks) are ignored.
func runCompactionCmd(cmd tea.Cmd) <-chan compactionDoneMsg {
	done := make(chan compactionDoneMsg, 1)
	var run func(tea.Cmd)
	run = func(c tea.Cmd) {
		if c == nil {
			return
		}
		go func() {
			msg := c()
			if batch, ok := msg.(tea.BatchMsg); ok {
				for _, sub := range batch {
					run(sub)
				}
				return
			}
			if d, ok := msg.(compactionDoneMsg); ok {
				done <- d
			}
		}()
	}
	run(cmd)
	return done
}

func waitForCompaction(t *testing.T, done <-chan compactionDoneMsg) compactionDoneMsg {
	t.Helper()
	select {
	case d := <-done:
		return d
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for the compactor")
		return compactionDoneMsg{}
	}
}

func writeFakeCompactor(t *testing.T, script string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fake-cake-compact")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestFinishRunCompactsInPlaceAndSeedsEstimate(t *testing.T) {
	argsPath := filepath.Join(t.TempDir(), "args")
	t.Setenv("ARGS_FILE", argsPath)
	bin := writeFakeCompactor(t, `
printf '%s\n' "$@" >> "$ARGS_FILE"
cat <<'JSON'
{"compacted":true,"source_session_id":"11111111-2222-3333-4444-555555555555","session_id":"11111111-2222-3333-4444-555555555555","in_place":true,"checkpoint_id":"c-1","boundary":4,"before_tokens":170000,"after_tokens":24000,"window":200000,"retained_count":4}
JSON
`)
	m := compactionModel(t, bin)

	tm, cmd := m.finishRun(cake.Result{ExitCode: 0})
	m = tm.(Model)
	if !m.compacting {
		t.Fatal("finishRun did not start a compaction")
	}
	if m.running {
		t.Fatal("run state survived finishRun")
	}
	if idx := compactionStartIndex(m); idx < 0 {
		t.Fatal("finishRun did not add a compaction start notice")
	} else if got := m.items[idx].Text; got != compactionStartText {
		t.Errorf("start notice = %q, want %q", got, compactionStartText)
	}
	if line := m.statusLine(); !strings.Contains(line, "compacting 0s") {
		t.Errorf("status line = %q, want elapsed compacting state", line)
	}
	if state := m.activityState(); state != "compacting 0s" {
		t.Errorf("composer state = %q, want compacting 0s", state)
	}
	if hint := m.composerHint(); !strings.Contains(hint, "Ctrl+C cancel") {
		t.Errorf("composer hint = %q, want the cancellation hint", hint)
	}
	if title, ok := titleSetBy(m.titleCmd()); !ok || !strings.HasPrefix(title, "[compacting] ") {
		t.Errorf("compacting title = %q, %v", title, ok)
	}
	assertCacheMatchesFullRender(t, &m, "after compaction start")

	// Prompt submission and session-target changes stay closed while it runs.
	m.input.SetValue("next prompt")
	tm, gateCmd := m.submit()
	m = tm.(Model)
	if gateCmd != nil {
		t.Errorf("submit returned a command while compacting: %v", gateCmd)
	}
	if it := lastItem(t, m); it.Kind != ui.KindWarning || !strings.Contains(it.Text, "context compaction is running") {
		t.Fatalf("submit while compacting = %+v", it)
	}
	tm, _ = m.execCommand(Command{Kind: CmdNew})
	m = tm.(Model)
	if it := lastItem(t, m); it.Kind != ui.KindWarning || !strings.Contains(it.Text, "wait for context compaction") {
		t.Fatalf("/new while compacting = %+v", it)
	}

	done := runCompactionCmd(cmd)
	tm, _ = m.Update(waitForCompaction(t, done))
	m = tm.(Model)

	if m.compacting {
		t.Fatal("compaction state survived the result")
	}
	if idx := compactionStartIndex(m); idx >= 0 {
		t.Fatalf("start notice survived the result: %+v", m.items[idx])
	}
	if line := m.statusLine(); strings.Contains(line, "compacting") {
		t.Errorf("status line kept the busy state: %q", line)
	}
	if mode, id := m.session.RunOptions(); mode != cake.RunResume || id != compactionSessionID {
		t.Fatalf("session changed to %v %q", mode, id)
	}
	if m.session.Context == nil || m.session.Context.Tokens != 24_000 ||
		m.session.Context.Window != 200_000 || !m.session.Context.Estimated {
		t.Fatalf("context = %+v, want the estimated after_tokens", m.session.Context)
	}
	if line := m.statusLine(); !strings.Contains(line, "context: ~24k/200k") {
		t.Errorf("status line = %q, want the estimate", line)
	}
	if it := lastItem(t, m); it.Kind != ui.KindInfo ||
		it.Text != "context compacted 170k -> 24k, 4 records retained" {
		t.Fatalf("timeline notice = %+v", it)
	}
	markers := 0
	for _, it := range m.items {
		if it.Kind == ui.KindInfo && strings.HasPrefix(it.Text, "context compacted") {
			markers++
		}
	}
	if markers != 1 {
		t.Fatalf("timeline has %d completion markers, want 1: %+v", markers, m.items)
	}
	if title, ok := titleSetBy(m.titleCmd()); !ok || strings.HasPrefix(title, "[") {
		t.Errorf("post-compaction title = %q, %v", title, ok)
	}
	if hint := m.composerHint(); !strings.Contains(hint, "Ctrl+S submit") {
		t.Errorf("post-compaction hint = %q, want the submit hint back", hint)
	}
	assertCacheMatchesFullRender(t, &m, "after compaction result")

	raw, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Fields(string(raw))
	want := []string{
		"compact", "-json", "-threshold", "-threshold-percent", "80",
		"-keep-tokens", "20000", "-cake-bin", "/opt/bin/cake",
		"-summarizer-model", "test-model", "-summarizer-profile", "test-profile",
		"--", compactionSessionID,
	}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("compactor args = %v, want %v", got, want)
	}
	if strings.Contains(string(raw), "-fork") {
		t.Fatalf("compactor was asked to fork: %s", raw)
	}
	if strings.Contains(string(raw), "-force") {
		t.Fatalf("compactor was asked to force: %s", raw)
	}
}

// TestFinishRunAbsoluteCompactionArgs checks the absolute trigger reaches the
// compactor as one -threshold-tokens flag carrying the same line the REPL used,
// including the window fallback, and that no percent, force, or fork flag leaks
// into the invocation.
func TestFinishRunAbsoluteCompactionArgs(t *testing.T) {
	tests := []struct {
		name        string
		tokens      int
		window      int
		wantTrigger string
	}{
		{"configured trigger on a large window", 360_000, 1_000_000, "350000"},
		{"window fallback caps the trigger", 200_000, 200_000, "150000"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			argsPath := filepath.Join(t.TempDir(), "args")
			t.Setenv("ARGS_FILE", argsPath)
			bin := writeFakeCompactor(t, `
printf '%s\n' "$@" >> "$ARGS_FILE"
cat <<'JSON'
{"compacted":true,"source_session_id":"11111111-2222-3333-4444-555555555555","session_id":"11111111-2222-3333-4444-555555555555","in_place":true,"checkpoint_id":"c-1","boundary":4,"before_tokens":360000,"after_tokens":24000,"window":1000000,"retained_count":4}
JSON
`)
			m := absoluteCompactionModel(t, bin, tt.tokens, tt.window)

			tm, cmd := m.finishRun(cake.Result{ExitCode: 0})
			m = tm.(Model)
			if !m.compacting {
				t.Fatal("finishRun did not start an absolute compaction")
			}
			tm, _ = m.Update(waitForCompaction(t, runCompactionCmd(cmd)))
			m = tm.(Model)
			if m.session.Context == nil || m.session.Context.Tokens != 24_000 || !m.session.Context.Estimated {
				t.Fatalf("context = %+v, want the estimated after_tokens", m.session.Context)
			}

			raw, err := os.ReadFile(argsPath)
			if err != nil {
				t.Fatal(err)
			}
			got := strings.Fields(string(raw))
			want := []string{
				"compact", "-json", "-threshold", "-threshold-tokens", tt.wantTrigger,
				"-keep-tokens", "20000", "-cake-bin", "/opt/bin/cake",
				"-summarizer-model", "test-model", "-summarizer-profile", "test-profile",
				"--", compactionSessionID,
			}
			if strings.Join(got, " ") != strings.Join(want, " ") {
				t.Fatalf("compactor args = %v, want %v", got, want)
			}
			for _, forbidden := range []string{"-threshold-percent", "-force", "-fork"} {
				if strings.Contains(string(raw), forbidden) {
					t.Fatalf("absolute compactor invocation carried %s: %s", forbidden, raw)
				}
			}
		})
	}
}

func TestCompactionNoOpKeepsContext(t *testing.T) {
	bin := writeFakeCompactor(t, `
cat <<'JSON'
{"compacted":false,"reason":"below threshold","source_session_id":"11111111-2222-3333-4444-555555555555","before_tokens":1000,"prefix_items":0,"retained_items":0}
JSON
`)
	m := compactionModel(t, bin)
	before := *m.session.Context

	tm, cmd := m.finishRun(cake.Result{ExitCode: 0})
	m = tm.(Model)
	tm, _ = m.Update(waitForCompaction(t, runCompactionCmd(cmd)))
	m = tm.(Model)

	if m.session.Context == nil || *m.session.Context != before {
		t.Fatalf("context = %+v, want unchanged %+v", m.session.Context, before)
	}
	if idx := compactionStartIndex(m); idx >= 0 {
		t.Fatalf("no-op left the start notice pending: %+v", m.items[idx])
	}
	if it := lastItem(t, m); it.Kind != ui.KindInfo || it.Text != "context compaction skipped: below threshold" {
		t.Fatalf("no-op notice = %+v", it)
	}
	if line := m.statusLine(); strings.Contains(line, "compacting") {
		t.Errorf("no-op left the busy state: %q", line)
	}
	assertCacheMatchesFullRender(t, &m, "after compaction no-op")
}

func TestCompactionFailureKeepsSessionAndWarns(t *testing.T) {
	bin := writeFakeCompactor(t, `echo "cake sessions checkpoint is unavailable; upgrade cake" >&2; exit 1`)
	m := compactionModel(t, bin)
	before := *m.session.Context

	tm, cmd := m.finishRun(cake.Result{ExitCode: 0})
	m = tm.(Model)
	tm, _ = m.Update(waitForCompaction(t, runCompactionCmd(cmd)))
	m = tm.(Model)

	if m.compacting {
		t.Fatal("failure left the gate closed")
	}
	if mode, id := m.session.RunOptions(); mode != cake.RunResume || id != compactionSessionID {
		t.Fatalf("session changed after failure: %v %q", mode, id)
	}
	if m.session.Context == nil || *m.session.Context != before {
		t.Fatalf("context changed after failure: %+v", m.session.Context)
	}
	if it := lastItem(t, m); it.Kind != ui.KindWarning ||
		!strings.Contains(it.Text, "context compaction failed") ||
		!strings.Contains(it.Text, "unavailable; upgrade cake") {
		t.Fatalf("failure notice = %+v", it)
	}
	if idx := compactionStartIndex(m); idx >= 0 {
		t.Fatalf("failure left the start notice pending: %+v", m.items[idx])
	}
}

func TestCompactionRejectsNonInPlaceResult(t *testing.T) {
	bin := writeFakeCompactor(t, `
cat <<'JSON'
{"compacted":true,"source_session_id":"11111111-2222-3333-4444-555555555555","session_id":"99999999-2222-3333-4444-555555555555","in_place":false,"before_tokens":170000,"after_tokens":1000}
JSON
`)
	m := compactionModel(t, bin)
	before := *m.session.Context

	tm, cmd := m.finishRun(cake.Result{ExitCode: 0})
	m = tm.(Model)
	tm, _ = m.Update(waitForCompaction(t, runCompactionCmd(cmd)))
	m = tm.(Model)

	if mode, id := m.session.RunOptions(); mode != cake.RunResume || id != compactionSessionID {
		t.Fatalf("non-in-place result retargeted the session: %v %q", mode, id)
	}
	if m.session.Context == nil || *m.session.Context != before {
		t.Fatalf("non-in-place result seeded context: %+v", m.session.Context)
	}
	if it := lastItem(t, m); it.Kind != ui.KindWarning || !strings.Contains(it.Text, "did not append in place") {
		t.Fatalf("non-in-place notice = %+v", it)
	}
	if idx := compactionStartIndex(m); idx >= 0 {
		t.Fatalf("non-in-place result left the start notice pending: %+v", m.items[idx])
	}
}

func TestCtrlCCancelsCompactionAndKeepsGateUntilReaped(t *testing.T) {
	bin := writeFakeCompactor(t, `exec sleep 30`)
	m := compactionModel(t, bin)

	tm, cmd := m.finishRun(cake.Result{ExitCode: 0})
	m = tm.(Model)
	done := runCompactionCmd(cmd)
	before := len(m.items)

	tm, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlC})
	m = tm.(Model)
	// Cancellation is reported once, when the child is reaped; the keypress
	// itself adds no notice, matching the run path. The start notice is still
	// pending while the child drains.
	if got := len(m.items); got != before {
		t.Fatalf("ctrl+c changed the timeline from %d to %d items", before, got)
	}
	if idx := compactionStartIndex(m); idx < 0 {
		t.Fatal("ctrl+c removed the pending start notice before the child was reaped")
	}
	if !m.compacting {
		t.Fatal("cancel reopened the gate before the child was reaped")
	}
	m.input.SetValue("next")
	if _, cmd := m.submit(); cmd != nil {
		t.Fatal("prompt submitted while the canceled child was draining")
	}

	tm, _ = m.Update(waitForCompaction(t, done))
	m = tm.(Model)
	if m.compacting {
		t.Fatal("reaped compaction left the gate closed")
	}
	if it := lastItem(t, m); it.Kind != ui.KindWarning || it.Text != "context compaction canceled" {
		t.Fatalf("canceled notice = %+v", it)
	}
	if idx := compactionStartIndex(m); idx >= 0 {
		t.Fatalf("canceled result left the start notice pending: %+v", m.items[idx])
	}
	if m.session.Context == nil || m.session.Context.Tokens != 170_000 {
		t.Fatalf("context changed on cancellation: %+v", m.session.Context)
	}
}

func TestExitWhileCompactingCancelsThenQuits(t *testing.T) {
	bin := writeFakeCompactor(t, `exec sleep 30`)
	m := compactionModel(t, bin)

	tm, compactionCmd := m.finishRun(cake.Result{ExitCode: 0})
	m = tm.(Model)
	done := runCompactionCmd(compactionCmd)

	tm, cmd := m.execCommand(Command{Kind: CmdExit})
	m = tm.(Model)
	if cmd != nil {
		t.Fatalf("/exit returned %v, want no command before the child is reaped", cmd)
	}
	if !m.exitAfter || !m.compacting {
		t.Fatalf("/exit state = exitAfter:%v compacting:%v", m.exitAfter, m.compacting)
	}
	if it := lastItem(t, m); it.Kind != ui.KindWarning || !strings.Contains(it.Text, "canceling context compaction, then exiting") {
		t.Fatalf("/exit notice = %+v", it)
	}

	tm, quitCmd := m.Update(waitForCompaction(t, done))
	m = tm.(Model)
	if m.compacting {
		t.Fatal("reaped cancellation left the gate closed")
	}
	if quitCmd == nil {
		t.Fatal("no quit command after the canceled child was reaped")
	}
	if _, ok := quitCmd().(tea.QuitMsg); !ok {
		t.Fatalf("quit command = %T, want tea.QuitMsg", quitCmd())
	}
}

func TestCtrlNWhileCompactingResetsWithoutApplyingResult(t *testing.T) {
	bin := writeFakeCompactor(t, `exec sleep 30`)
	m := compactionModel(t, bin)

	tm, cmd := m.finishRun(cake.Result{ExitCode: 0})
	m = tm.(Model)
	done := runCompactionCmd(cmd)

	tm, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlN})
	m = tm.(Model)
	if !m.newSessionPending {
		t.Fatal("ctrl+n did not mark the canceled compaction for draining")
	}
	if mode, id := m.session.RunOptions(); mode != cake.RunFresh || id != "" {
		t.Fatalf("ctrl+n session state = %v %q", mode, id)
	}

	tm, _ = m.Update(waitForCompaction(t, done))
	m = tm.(Model)
	if m.compacting || m.newSessionPending {
		t.Fatalf("drained compaction state = compacting:%v pending:%v", m.compacting, m.newSessionPending)
	}
	if m.session.Context != nil {
		t.Fatalf("stale compaction result seeded the new session: %+v", m.session.Context)
	}
	if len(m.items) != 1 || m.items[0].Text != "New session" {
		t.Fatalf("timeline = %#v, want only the new-session boundary", m.items)
	}
}

func TestCompactionSkippedForNoSession(t *testing.T) {
	bin := writeFakeCompactor(t, `touch "$ARGS_FILE"`)
	argsPath := filepath.Join(t.TempDir(), "args")
	t.Setenv("ARGS_FILE", argsPath)
	m := compactionModel(t, bin)
	m.cfg.NoSession = true

	tm, cmd := m.finishRun(cake.Result{ExitCode: 0})
	m = tm.(Model)
	if m.compacting {
		t.Fatal("compaction started with -no-session")
	}
	if cmd == nil {
		t.Fatal("finishRun returned no title command")
	}
	if _, err := os.Stat(argsPath); !os.IsNotExist(err) {
		t.Fatalf("compactor was invoked for -no-session: %v", err)
	}
}

// TestCompactionElapsedAdvancesOnSpinnerTick checks the visible age is driven by
// the spinner chain the REPL already runs: no second timer is started, and the
// status line and composer agree on the value.
func TestCompactionElapsedAdvancesOnSpinnerTick(t *testing.T) {
	bin := writeFakeCompactor(t, `exec sleep 30`)
	m := compactionModel(t, bin)

	tm, _ := m.finishRun(cake.Result{ExitCode: 0})
	m = tm.(Model)
	start := m.compactionStart
	if start.IsZero() {
		t.Fatal("compaction start time was not recorded")
	}
	if line := m.statusLine(); !strings.Contains(line, "compacting 0s") {
		t.Fatalf("initial status line = %q, want compacting 0s", line)
	}

	tm, cmd := m.Update(spinner.TickMsg{Time: start.Add(5 * time.Second)})
	m = tm.(Model)
	if m.compactionElapsed != 5*time.Second {
		t.Errorf("elapsed = %v, want 5s", m.compactionElapsed)
	}
	if cmd == nil {
		t.Fatal("spinner tick chain stopped while compacting")
	}
	if line := m.statusLine(); !strings.Contains(line, "compacting 5s") {
		t.Errorf("status line = %q, want compacting 5s", line)
	}
	if state := m.activityState(); state != "compacting 5s" {
		t.Errorf("composer state = %q, want compacting 5s", state)
	}
}

func TestCompactionElapsedText(t *testing.T) {
	tests := []struct {
		in   time.Duration
		want string
	}{
		{0, "0s"},
		{-time.Second, "0s"},
		{time.Second, "1s"},
		{59 * time.Second, "59s"},
		{60 * time.Second, "1m00s"},
		{65 * time.Second, "1m05s"},
		{time.Hour, "1h00m00s"},
	}
	for _, tt := range tests {
		if got := compactionElapsedText(tt.in); got != tt.want {
			t.Errorf("compactionElapsedText(%v) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// TestCompactionViewNarrowNoColor drives the real View at narrow widths under
// the Ascii profile: the pending start notice, cancellation hint, and elapsed
// state must be on screen without overflow or escapes, and the resolved view
// must replace the notice with the one outcome row.
func TestCompactionViewNarrowNoColor(t *testing.T) {
	orig := lipgloss.ColorProfile()
	defer lipgloss.SetColorProfile(orig)
	lipgloss.SetColorProfile(termenv.Ascii)

	for _, width := range []int{24, 32, 60} {
		t.Run(fmt.Sprintf("width %d", width), func(t *testing.T) {
			bin := writeFakeCompactor(t, `
cat <<'JSON'
{"compacted":true,"source_session_id":"11111111-2222-3333-4444-555555555555","session_id":"11111111-2222-3333-4444-555555555555","in_place":true,"checkpoint_id":"c-1","boundary":4,"before_tokens":170000,"after_tokens":24000,"window":200000,"retained_count":4}
JSON
`)
			m := compactionModel(t, bin)
			m.width, m.height = width, 30
			m.layout()

			tm, cmd := m.finishRun(cake.Result{ExitCode: 0})
			m = tm.(Model)
			pending := m.View()
			assertNoANSIOrOverflow(t, pending, width)
			for _, want := range []string{"context compaction", "Ctrl+C", "compacting 0s"} {
				if !strings.Contains(pending, want) {
					t.Errorf("pending view missing %q:\n%s", want, pending)
				}
			}

			// Advance the existing spinner clock and check the elapsed age is
			// visible at this width too; the PTY capture combines the same states.
			tm, _ = m.Update(spinner.TickMsg{Time: m.compactionStart.Add(3 * time.Second)})
			m = tm.(Model)
			progress := m.View()
			assertNoANSIOrOverflow(t, progress, width)
			if !strings.Contains(progress, "compacting 3s") {
				t.Errorf("progress view missing compacting 3s:\n%s", progress)
			}

			tm, _ = m.Update(waitForCompaction(t, runCompactionCmd(cmd)))
			m = tm.(Model)
			resolved := m.View()
			assertNoANSIOrOverflow(t, resolved, width)
			if !strings.Contains(resolved, "context compacted") {
				t.Errorf("resolved view missing the outcome row:\n%s", resolved)
			}
			if strings.Contains(resolved, "context compaction started") {
				t.Errorf("resolved view kept the start notice:\n%s", resolved)
			}
		})
	}
}

// assertNoANSIOrOverflow checks one rendered frame for the two properties the
// no-color narrow captures promise: no ANSI escapes and no row wider than the
// terminal.
func assertNoANSIOrOverflow(t *testing.T, view string, width int) {
	t.Helper()
	if strings.Contains(view, "\x1b[") {
		t.Errorf("Ascii view contains ANSI escapes:\n%q", view)
	}
	for _, line := range strings.Split(view, "\n") {
		if w := lipgloss.Width(line); w > width {
			t.Errorf("line %d cells wide, want <= %d: %q", w, width, line)
		}
	}
}

// TestCompactionSuccessAfterClearIsStillReported checks that /clear during a
// compaction leaves no stale start row, and that the success outcome is still
// reported once the child is reaped.
func TestCompactionSuccessAfterClearIsStillReported(t *testing.T) {
	bin := writeFakeCompactor(t, `
cat <<'JSON'
{"compacted":true,"source_session_id":"11111111-2222-3333-4444-555555555555","session_id":"11111111-2222-3333-4444-555555555555","in_place":true,"checkpoint_id":"c-1","boundary":4,"before_tokens":170000,"after_tokens":24000,"window":200000,"retained_count":4}
JSON
`)
	m := compactionModel(t, bin)

	tm, cmd := m.finishRun(cake.Result{ExitCode: 0})
	m = tm.(Model)
	done := runCompactionCmd(cmd)

	tm, _ = m.execCommand(Command{Kind: CmdClear})
	m = tm.(Model)
	if len(m.items) != 0 {
		t.Fatalf("timeline after /clear = %+v, want empty", m.items)
	}

	tm, _ = m.Update(waitForCompaction(t, done))
	m = tm.(Model)
	if it := lastItem(t, m); it.Kind != ui.KindInfo ||
		it.Text != "context compacted 170k -> 24k, 4 records retained" {
		t.Fatalf("post-clear outcome = %+v", it)
	}
	if compactionStartIndex(m) >= 0 {
		t.Fatal("start notice reappeared after /clear")
	}
	assertCacheMatchesFullRender(t, &m, "after post-clear compaction result")
}

// TestCompactionNoOpAfterClearStaysSilent preserves the old silent no-op when
// the start notice it would have resolved was cleared away.
func TestCompactionNoOpAfterClearStaysSilent(t *testing.T) {
	bin := writeFakeCompactor(t, `
cat <<'JSON'
{"compacted":false,"reason":"below threshold","source_session_id":"11111111-2222-3333-4444-555555555555"}
JSON
`)
	m := compactionModel(t, bin)

	tm, cmd := m.finishRun(cake.Result{ExitCode: 0})
	m = tm.(Model)
	done := runCompactionCmd(cmd)

	tm, _ = m.execCommand(Command{Kind: CmdClear})
	m = tm.(Model)
	tm, _ = m.Update(waitForCompaction(t, done))
	m = tm.(Model)
	if len(m.items) != 0 {
		t.Fatalf("cleared no-op added a timeline item: %+v", m.items)
	}
}
