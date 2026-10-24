package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/travisennis/cake-repl/internal/cake"
	"github.com/travisennis/cake-repl/internal/ui"
)

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

// compactionModel returns a model whose last turn completed with a context over
// the trigger, as finishRun sees it.
func compactionModel(t *testing.T, bin string) Model {
	t.Helper()
	m := laidOut(160, 24)
	m.cfg.Compaction = CompactionConfig{Enabled: true, Bin: bin, ThresholdPercent: 80, KeepTokens: 20000}
	m.cfg.CakeBin = "/opt/bin/cake"
	m.cfg.Model = "test-model"
	m.session.OnTaskStart(cake.TaskStart{SessionID: compactionSessionID, TaskID: "t-1"})
	m.applyEvent(cake.TurnUsage{
		Usage:         cake.Usage{InputTokens: 170_000},
		UsagePresence: "complete",
		ContextWindow: 200_000,
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
	if line := m.statusLine(); !strings.Contains(line, "compacting") {
		t.Errorf("status line = %q, want compacting state", line)
	}
	if title, ok := titleSetBy(m.titleCmd()); !ok || !strings.HasPrefix(title, "[compacting] ") {
		t.Errorf("compacting title = %q, %v", title, ok)
	}

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
	if title, ok := titleSetBy(m.titleCmd()); !ok || strings.HasPrefix(title, "[") {
		t.Errorf("post-compaction title = %q, %v", title, ok)
	}

	raw, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Fields(string(raw))
	want := []string{
		"compact", "-json", "-threshold", "-threshold-percent", "80",
		"-keep-tokens", "20000", "-cake-bin", "/opt/bin/cake",
		"-summarizer-model", "test-model", "--", compactionSessionID,
	}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("compactor args = %v, want %v", got, want)
	}
	if strings.Contains(string(raw), "-fork") {
		t.Fatalf("compactor was asked to fork: %s", raw)
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
	if it := lastItem(t, m); it.Kind != ui.KindComplete {
		t.Fatalf("no-op added a timeline item: %+v", it)
	}
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
}

func TestCtrlCCancelsCompactionAndKeepsGateUntilReaped(t *testing.T) {
	bin := writeFakeCompactor(t, `exec sleep 30`)
	m := compactionModel(t, bin)

	tm, cmd := m.finishRun(cake.Result{ExitCode: 0})
	m = tm.(Model)
	done := runCompactionCmd(cmd)

	tm, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlC})
	m = tm.(Model)
	if it := lastItem(t, m); it.Kind != ui.KindWarning || !strings.Contains(it.Text, "canceling context compaction") {
		t.Fatalf("ctrl+c notice = %+v", it)
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
