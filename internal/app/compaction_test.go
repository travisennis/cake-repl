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
// model holds no attached notice. It follows the stable handle, so an unrelated
// info row with the same text cannot stand in for the live row.
func compactionStartIndex(m Model) int {
	return noticeItemIndex(m, m.compactionNotice)
}

// noticeItemIndex returns the row carrying id, or -1 when it is no longer
// attached. Zero never identifies a row.
func noticeItemIndex(m Model, id noticeID) int {
	if id == 0 {
		return -1
	}
	for i := len(m.timeline.items) - 1; i >= 0; i-- {
		if noticeID(m.timeline.items[i].ID) == id {
			return i
		}
	}
	return -1
}

// assertCompactionIdle checks that one settled result left no phase, request,
// or notice handle behind.
func assertCompactionIdle(t *testing.T, m Model) {
	t.Helper()
	if m.compaction.active() || m.compaction.draining() || m.compaction.key.request != 0 || m.compactionNotice != 0 {
		t.Fatalf("compaction not idle: active=%v draining=%v request=%d notice=%d",
			m.compaction.active(), m.compaction.draining(), m.compaction.key.request, m.compactionNotice)
	}
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
		markCompletedTurn(&m)
		return m
	}

	tests := []struct {
		name  string
		setup func(*Model)
		want  bool
	}{
		{"threshold crossed", func(*Model) {}, true},
		{"below the trigger headroom", func(m *Model) {
			m.session.Context.Tokens = 149_000 // trigger is 75% of 200k
		}, false},
		{"at the trigger headroom", func(m *Model) {
			m.session.Context.Tokens = 150_000
		}, true},
		{"estimated figure is re-checked", func(m *Model) {
			m.session.Context.Estimated = true
		}, true},
		{"no window", func(m *Model) { m.session.Context.Window = 0 }, false},
		{"no measurement", func(m *Model) { m.session.Context = nil }, false},
		{"no pinned session", func(m *Model) { m.session.Reset() }, false},
		{"incomplete turn", func(m *Model) { m.runOutcome = outcomeFailure }, false},
		{"errored completion", func(m *Model) {
			m.session.LastComplete.IsError = true
			m.runOutcome = outcomeFailure
		}, false},
		{"canceled run", func(m *Model) { m.runOutcome = outcomeCanceled }, false},
		{"failed run", func(m *Model) { m.runOutcome = outcomeFailure }, false},
		{"wait error", func(m *Model) { m.runOutcome = outcomeFailure }, false},
		{"disabled", func(m *Model) { m.cfg.Compaction.Enabled = false }, false},
		{"no session", func(m *Model) { m.cfg.NoSession = true }, false},
		{"invalid threshold", func(m *Model) { m.cfg.Compaction.ThresholdPercent = 0 }, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := ready(t)
			tt.setup(&m)
			if got := m.shouldCompactAfter(); got != tt.want {
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
		markCompletedTurn(&m)
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
			if got := m.shouldCompactAfter(); got != tt.want {
				t.Fatalf("shouldCompactAfter = %v, want %v (context %+v)", got, tt.want, m.session.Context)
			}
		})
	}

	t.Run("incomplete absolute config disables the trigger", func(t *testing.T) {
		m := ready(900_000, 1_000_000)
		m.cfg.Compaction.TriggerTokens = 0
		if m.shouldCompactAfter() {
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
	m.turn.Model = "test-model"
	m.turn.Profile = "test-profile"
	m.session.OnTaskStart(cake.TaskStart{SessionID: compactionSessionID, TaskID: "t-1"})
	m.applyEvent(cake.TurnUsage{
		Usage:         cake.Usage{InputTokens: tokens},
		UsagePresence: "complete",
		ContextWindow: window,
	})
	m.applyEvent(cake.TaskComplete{Subtype: "success", SessionID: compactionSessionID, TaskID: "t-1"})
	beginTestRun(&m)
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
	if !m.compactionBusy() || !m.compaction.active() || m.compaction.draining() {
		t.Fatalf("compaction phase = active:%v draining:%v, want in-flight", m.compaction.active(), m.compaction.draining())
	}
	if m.runBusy() {
		t.Fatal("run state survived finishRun")
	}
	if idx := compactionStartIndex(m); idx < 0 {
		t.Fatal("finishRun did not add a compaction start notice")
	} else if got := m.timeline.items[idx].Text; got != compactionStartText {
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

	if m.compactionBusy() || m.compaction.active() || m.compaction.draining() {
		t.Fatalf("compaction phase after result = active:%v draining:%v, want idle", m.compaction.active(), m.compaction.draining())
	}
	if m.compaction.key.request != 0 || m.compactionNotice != 0 {
		t.Fatalf("compaction identity survived the result: request=%d notice=%d", m.compaction.key.request, m.compactionNotice)
	}
	if idx := compactionStartIndex(m); idx >= 0 {
		t.Fatalf("start notice survived the result: %+v", m.timeline.items[idx])
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
	for _, it := range m.timeline.items {
		if it.Kind == ui.KindInfo && strings.HasPrefix(it.Text, "context compacted") {
			markers++
		}
	}
	if markers != 1 {
		t.Fatalf("timeline has %d completion markers, want 1: %+v", markers, m.timeline.items)
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

// TestCompactionStartNoticeUsesStableIdentity proves the completion path tracks
// the live row by its assigned identity. An unrelated info row with the same
// text arrives after the start notice; a last-prefix lookup would resolve that
// row and leave the live notice reading as pending.
func TestCompactionStartNoticeUsesStableIdentity(t *testing.T) {
	bin := writeFakeCompactor(t, `
cat <<'JSON'
{"compacted":true,"source_session_id":"11111111-2222-3333-4444-555555555555","session_id":"11111111-2222-3333-4444-555555555555","in_place":true,"checkpoint_id":"c-1","boundary":4,"before_tokens":170000,"after_tokens":24000,"window":200000,"retained_count":4}
JSON
`)
	m := compactionModel(t, bin)

	tm, cmd := m.finishRun(cake.Result{ExitCode: 0})
	m = tm.(Model)
	live := m.compactionNotice
	if live == 0 || compactionStartIndex(m) < 0 {
		t.Fatal("finishRun did not attach a live start notice")
	}

	// Shift the live row and then add a historical row with the same text after
	// it. Only the stable identity distinguishes the two.
	m.insertItemAt(0, ui.Item{Kind: ui.KindInfo, Text: "earlier history"})
	m.appendItem(ui.Item{Kind: ui.KindInfo, Text: compactionStartText})

	tm, _ = m.Update(waitForCompaction(t, runCompactionCmd(cmd)))
	m = tm.(Model)

	if noticeItemIndex(m, live) >= 0 {
		t.Fatal("live start notice survived the result")
	}
	if m.compactionNotice != 0 {
		t.Fatalf("compaction notice handle = %d after resolution, want 0", m.compactionNotice)
	}
	starts := 0
	for _, it := range m.timeline.items {
		if it.Text == compactionStartText {
			starts++
		}
	}
	if starts != 1 {
		t.Fatalf("timeline has %d rows with the start text, want the one unrelated row: %+v", starts, m.timeline.items)
	}
	if it := lastItem(t, m); it.Text != "context compacted 170k -> 24k, 4 records retained" {
		t.Fatalf("outcome notice = %+v", it)
	}
}

// TestCompactionDuplicateResultCannotApplyTwice covers a replayed terminal
// message after the real result has settled the request. The cleared request
// identity must make the second result a no-op instead of appending another
// marker or changing the context estimate.
func TestCompactionDuplicateResultCannotApplyTwice(t *testing.T) {
	bin := writeFakeCompactor(t, `
cat <<'JSON'
{"compacted":true,"source_session_id":"11111111-2222-3333-4444-555555555555","session_id":"11111111-2222-3333-4444-555555555555","in_place":true,"checkpoint_id":"c-1","boundary":4,"before_tokens":170000,"after_tokens":24000,"window":200000,"retained_count":4}
JSON
`)
	m := compactionModel(t, bin)

	tm, cmd := m.finishRun(cake.Result{ExitCode: 0})
	m = tm.(Model)
	msg := waitForCompaction(t, runCompactionCmd(cmd))
	tm, firstCmd := m.Update(msg)
	m = tm.(Model)
	if firstCmd == nil || m.compactionBusy() {
		t.Fatalf("first result did not settle: busy=%v cmd nil=%v", m.compactionBusy(), firstCmd == nil)
	}
	before := *m.session.Context
	items := len(m.timeline.items)
	markers := compactionMarkerCount(m)

	tm, duplicateCmd := m.Update(msg)
	m = tm.(Model)
	if duplicateCmd != nil {
		t.Fatalf("duplicate result returned a command: %v", duplicateCmd)
	}
	if len(m.timeline.items) != items {
		t.Fatalf("duplicate result changed the timeline: %d -> %d items", items, len(m.timeline.items))
	}
	if got := *m.session.Context; got != before {
		t.Fatalf("duplicate result changed the context: %+v -> %+v", before, got)
	}
	if got := compactionMarkerCount(m); got != markers {
		t.Fatalf("duplicate result changed the marker count: %d -> %d", markers, got)
	}
}

func compactionMarkerCount(m Model) int {
	markers := 0
	for _, it := range m.timeline.items {
		if it.Kind == ui.KindInfo && strings.HasPrefix(it.Text, "context compacted") {
			markers++
		}
	}
	return markers
}

// TestFinishRunUsesCompactionSummarizerSelection proves the compaction table's
// model/profile/effort override the turn selection without changing the
// compactor's fixed argument shape.
func TestFinishRunUsesCompactionSummarizerSelection(t *testing.T) {
	argsPath := filepath.Join(t.TempDir(), "args")
	t.Setenv("ARGS_FILE", argsPath)
	bin := writeFakeCompactor(t, `
printf '%s\n' "$@" >> "$ARGS_FILE"
cat <<'JSON'
{"compacted":true,"source_session_id":"11111111-2222-3333-4444-555555555555","session_id":"11111111-2222-3333-4444-555555555555","in_place":true,"checkpoint_id":"c-1","boundary":4,"before_tokens":170000,"after_tokens":24000,"window":200000,"retained_count":4}
JSON
`)
	m := compactionModelWithConfig(t, CompactionConfig{
		Enabled:          true,
		Bin:              bin,
		ThresholdPercent: 80,
		KeepTokens:       20000,
		Model:            "compact-model",
		Profile:          "compact-profile",
		ReasoningEffort:  "low",
	}, 170_000, 200_000)

	tm, cmd := m.finishRun(cake.Result{ExitCode: 0})
	m = tm.(Model)
	m.Update(waitForCompaction(t, runCompactionCmd(cmd)))

	raw, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Fields(string(raw))
	want := []string{
		"compact", "-json", "-threshold", "-threshold-percent", "80",
		"-keep-tokens", "20000", "-cake-bin", "/opt/bin/cake",
		"-summarizer-model", "compact-model",
		"-summarizer-profile", "compact-profile",
		"-summarizer-reasoning-effort", "low",
		"--", compactionSessionID,
	}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("compactor args = %v, want %v", got, want)
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
			if !m.compactionBusy() {
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

	assertCompactionIdle(t, m)
	if m.session.Context == nil || *m.session.Context != before {
		t.Fatalf("context = %+v, want unchanged %+v", m.session.Context, before)
	}
	if idx := compactionStartIndex(m); idx >= 0 {
		t.Fatalf("no-op left the start notice pending: %+v", m.timeline.items[idx])
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

	assertCompactionIdle(t, m)
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
		t.Fatalf("failure left the start notice pending: %+v", m.timeline.items[idx])
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

	assertCompactionIdle(t, m)
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
		t.Fatalf("non-in-place result left the start notice pending: %+v", m.timeline.items[idx])
	}
}

func TestCtrlCCancelsCompactionAndKeepsGateUntilReaped(t *testing.T) {
	bin := writeFakeCompactor(t, `exec sleep 30`)
	m := compactionModel(t, bin)

	tm, cmd := m.finishRun(cake.Result{ExitCode: 0})
	m = tm.(Model)
	done := runCompactionCmd(cmd)
	before := len(m.timeline.items)

	tm, cmd = m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlC})
	m = tm.(Model)
	requireTitle(t, cmd, windowTitle(m.cfg.Cwd, "[stopping]"))
	// Cancellation is reported once, when the child is reaped; the keypress
	// itself adds no notice, matching the run path. The start notice is still
	// pending while the child drains.
	if got := len(m.timeline.items); got != before {
		t.Fatalf("ctrl+c changed the timeline from %d to %d items", before, got)
	}
	if idx := compactionStartIndex(m); idx < 0 {
		t.Fatal("ctrl+c removed the pending start notice before the child was reaped")
	}
	if !m.compactionBusy() || !m.compaction.draining() || m.compaction.active() {
		t.Fatalf("cancel phase = active:%v draining:%v, want draining", m.compaction.active(), m.compaction.draining())
	}
	m.input.SetValue("next")
	tm, submitCmd := m.submit()
	m = tm.(Model)
	if submitCmd != nil {
		t.Fatal("prompt submitted while the canceled child was draining")
	}
	if it := lastItem(t, m); it.Kind != ui.KindWarning || !strings.Contains(it.Text, "context compaction") {
		t.Fatalf("submit while the canceled compactor drained = %+v", it)
	}
	for _, tt := range []struct {
		name string
		cmd  Command
	}{
		{"/new", Command{Kind: CmdNew}},
		{"/resume", Command{Kind: CmdResume, Arg: "99999999-2222-3333-4444-555555555555"}},
		{"/sessions", Command{Kind: CmdSessions}},
	} {
		next, retargetCmd := m.execCommand(tt.cmd)
		m = next.(Model)
		if retargetCmd != nil {
			t.Fatalf("%s returned a command while the canceled compactor drained: %v", tt.name, retargetCmd)
		}
		if it := lastItem(t, m); it.Kind != ui.KindWarning || !strings.Contains(it.Text, "canceled context compaction") {
			t.Fatalf("%s while the canceled compactor drained = %+v", tt.name, it)
		}
	}
	if m.browser.open() {
		t.Fatal("/sessions opened the browser while the canceled compactor drained")
	}
	if mode, id := m.session.RunOptions(); mode != cake.RunResume || id != compactionSessionID {
		t.Fatalf("refused retarget changed the session: %v %q", mode, id)
	}

	tm, _ = m.Update(waitForCompaction(t, done))
	m = tm.(Model)
	if m.compactionBusy() || m.compaction.active() || m.compaction.draining() {
		t.Fatalf("reaped compaction phase = active:%v draining:%v, want idle", m.compaction.active(), m.compaction.draining())
	}
	if m.compaction.key.request != 0 || m.compactionNotice != 0 {
		t.Fatalf("compaction identity survived cancellation: request=%d notice=%d", m.compaction.key.request, m.compactionNotice)
	}
	if it := lastItem(t, m); it.Kind != ui.KindWarning || it.Text != "context compaction canceled" {
		t.Fatalf("canceled notice = %+v", it)
	}
	if idx := compactionStartIndex(m); idx >= 0 {
		t.Fatalf("canceled result left the start notice pending: %+v", m.timeline.items[idx])
	}
	if m.session.Context == nil || m.session.Context.Tokens != 170_000 {
		t.Fatalf("context changed on cancellation: %+v", m.session.Context)
	}

	tm, _ = m.execCommand(Command{Kind: CmdNew})
	m = tm.(Model)
	if mode, id := m.session.RunOptions(); mode != cake.RunFresh || id != "" {
		t.Fatalf("compaction gate stayed closed after the child was reaped: %v %q", mode, id)
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
	requireTitle(t, cmd, windowTitle(m.cfg.Cwd, "[exiting]"))
	if !m.exiting() || !m.compactionBusy() {
		t.Fatalf("/exit state = exiting:%v compacting:%v", m.exiting(), m.compactionBusy())
	}
	if it := lastItem(t, m); it.Kind != ui.KindWarning || !strings.Contains(it.Text, "canceling context compaction, then exiting") {
		t.Fatalf("/exit notice = %+v", it)
	}

	tm, quitCmd := m.Update(waitForCompaction(t, done))
	m = tm.(Model)
	if m.compactionBusy() {
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
	generation := m.session.Generation()

	tm, cmd = m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlN})
	m = tm.(Model)
	requireTitle(t, cmd, windowTitle(m.cfg.Cwd, "[stopping]"))
	if !m.compaction.draining() {
		t.Fatal("ctrl+n did not put the canceled compaction into its drain phase")
	}
	if m.session.Generation() <= generation {
		t.Fatalf("ctrl+n did not advance the session generation: %d -> %d", generation, m.session.Generation())
	}
	if mode, id := m.session.RunOptions(); mode != cake.RunFresh || id != "" {
		t.Fatalf("ctrl+n session state = %v %q", mode, id)
	}

	tm, _ = m.Update(waitForCompaction(t, done))
	m = tm.(Model)
	assertCompactionIdle(t, m)
	if m.session.Context != nil {
		t.Fatalf("stale compaction result seeded the new session: %+v", m.session.Context)
	}
	if len(m.timeline.items) != 1 || m.timeline.items[0].Text != "New session" {
		t.Fatalf("timeline = %#v, want only the new-session boundary", m.timeline.items)
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
	if m.compactionBusy() {
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
	if len(m.timeline.items) != 0 {
		t.Fatalf("timeline after /clear = %+v, want empty", m.timeline.items)
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
	if len(m.timeline.items) != 0 {
		t.Fatalf("cleared no-op added a timeline item: %+v", m.timeline.items)
	}
}
