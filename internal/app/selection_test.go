package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/travisennis/cake-repl/internal/ui"
)

func TestModelCommandShowsSetsAndClearsSelection(t *testing.T) {
	m := New(Config{Model: "startup-model"})
	m.width, m.height = 80, 24
	m.layout()

	tm, _ := m.execCommand(Command{Kind: CmdModel})
	m = tm.(Model)
	if it := lastItem(t, m); it.Kind != ui.KindInfo || it.Text != "model: startup-model" {
		t.Fatalf("status item = %+v, want the startup model", it)
	}

	m.session.Model = "reported-model"
	m.session.Context = &ContextUsage{Tokens: 1000, Window: 2000}
	tm, _ = m.execCommand(Command{Kind: CmdModel, Arg: "gpt-x"})
	m = tm.(Model)
	if m.turn.Model != "gpt-x" {
		t.Errorf("turn model = %q, want gpt-x", m.turn.Model)
	}
	if m.session.Model != "" {
		t.Errorf("reported model = %q, want cleared by the selection change", m.session.Model)
	}
	if m.session.Context != nil {
		t.Errorf("context = %+v, want cleared by the selection change", m.session.Context)
	}
	if got := m.displayModel(); got != "gpt-x" {
		t.Errorf("display model = %q, want gpt-x", got)
	}
	if it := lastItem(t, m); it.Kind != ui.KindInfo || it.Text != "next prompt model: gpt-x" {
		t.Fatalf("set item = %+v", it)
	}
	if line := m.statusLine(); !strings.Contains(line, "model: gpt-x") {
		t.Errorf("status line = %q, want the selected model", line)
	}

	tm, _ = m.execCommand(Command{Kind: CmdModel})
	m = tm.(Model)
	if it := lastItem(t, m); it.Text != "model: gpt-x" {
		t.Fatalf("status item = %+v, want the selected model", it)
	}

	tm, _ = m.execCommand(Command{Kind: CmdModel, Arg: "default"})
	m = tm.(Model)
	if m.turn.Model != "" {
		t.Errorf("turn model = %q, want cleared", m.turn.Model)
	}
	if got := m.displayModel(); got != "" {
		t.Errorf("display model = %q, want empty", got)
	}
	if it := lastItem(t, m); it.Kind != ui.KindInfo || it.Text != "next prompt model: (cake default)" {
		t.Fatalf("clear item = %+v", it)
	}
	if line := m.statusLine(); strings.Contains(line, "model:") {
		t.Errorf("status line = %q, want the model segment omitted", line)
	}

	tm, _ = m.execCommand(Command{Kind: CmdModel})
	m = tm.(Model)
	if it := lastItem(t, m); it.Text != "model: (cake default)" {
		t.Fatalf("status item after clear = %+v", it)
	}
}

func TestProfileCommandShowsSetsAndClearsSelection(t *testing.T) {
	m := New(Config{Profile: "startup-profile"})
	m.width, m.height = 80, 24
	m.layout()

	tm, _ := m.execCommand(Command{Kind: CmdProfile})
	m = tm.(Model)
	if it := lastItem(t, m); it.Kind != ui.KindInfo || it.Text != "profile: startup-profile" {
		t.Fatalf("status item = %+v, want the startup profile", it)
	}

	m.session.Model = "reported-model"
	m.session.Context = &ContextUsage{Tokens: 1000, Window: 2000}
	tm, _ = m.execCommand(Command{Kind: CmdProfile, Arg: "work"})
	m = tm.(Model)
	if m.turn.Profile != "work" {
		t.Errorf("turn profile = %q, want work", m.turn.Profile)
	}
	if m.session.Model != "" {
		t.Errorf("reported model = %q, want cleared by the selection change", m.session.Model)
	}
	if m.session.Context != nil {
		t.Errorf("context = %+v, want cleared by the selection change", m.session.Context)
	}
	if it := lastItem(t, m); it.Kind != ui.KindInfo || it.Text != "next prompt profile: work" {
		t.Fatalf("set item = %+v", it)
	}

	tm, _ = m.execCommand(Command{Kind: CmdProfile})
	m = tm.(Model)
	if it := lastItem(t, m); it.Text != "profile: work" {
		t.Fatalf("status item = %+v, want the selected profile", it)
	}

	tm, _ = m.execCommand(Command{Kind: CmdProfile, Arg: "default"})
	m = tm.(Model)
	if m.turn.Profile != "" {
		t.Errorf("turn profile = %q, want cleared", m.turn.Profile)
	}
	if it := lastItem(t, m); it.Kind != ui.KindInfo || it.Text != "next prompt profile: (cake default)" {
		t.Fatalf("clear item = %+v", it)
	}

	tm, _ = m.execCommand(Command{Kind: CmdProfile})
	m = tm.(Model)
	if it := lastItem(t, m); it.Text != "profile: (cake default)" {
		t.Fatalf("status item after clear = %+v", it)
	}
}

func TestModelAndProfileCommandsRequireIdle(t *testing.T) {
	m := newLaidOutModel()
	m.turn = turnSelection{Model: "startup-model", Profile: "startup-profile"}
	beginTestRun(&m)

	for _, cmd := range []Command{
		{Kind: CmdModel, Arg: "gpt-x"},
		{Kind: CmdProfile, Arg: "work"},
	} {
		tm, _ := m.execCommand(cmd)
		m = tm.(Model)
		it := lastItem(t, m)
		if it.Kind != ui.KindWarning || !strings.Contains(it.Text, "finish or cancel the running task first") {
			t.Errorf("command %v item = %+v, want the idle refusal", cmd, it)
		}
	}
	if m.turn.Model != "startup-model" || m.turn.Profile != "startup-profile" {
		t.Errorf("selection changed during a run: model=%q profile=%q", m.turn.Model, m.turn.Profile)
	}
}

func TestModelAndProfileSelectionSurvivesNewAndResume(t *testing.T) {
	const id = "11111111-2222-3333-4444-555555555555"
	m := newLaidOutModel()
	tm, _ := m.execCommand(Command{Kind: CmdModel, Arg: "gpt-x"})
	m = tm.(Model)
	tm, _ = m.execCommand(Command{Kind: CmdProfile, Arg: "work"})
	m = tm.(Model)

	tm, _ = m.execCommand(Command{Kind: CmdNew})
	m = tm.(Model)
	if m.turn.Model != "gpt-x" || m.turn.Profile != "work" {
		t.Errorf("after /new selection = %+v, want preserved", m.turn)
	}

	tm, _ = m.execCommand(Command{Kind: CmdResume, Arg: id})
	m = tm.(Model)
	if m.turn.Model != "gpt-x" || m.turn.Profile != "work" {
		t.Errorf("after /resume selection = %+v, want preserved", m.turn)
	}
}

func TestStartRunUsesTurnSelection(t *testing.T) {
	tests := []struct {
		name        string
		commands    []Command
		wantModel   string
		wantProfile string
	}{
		{
			name: "selected values",
			commands: []Command{
				{Kind: CmdModel, Arg: "gpt-x"},
				{Kind: CmdProfile, Arg: "work"},
			},
			wantModel:   "gpt-x",
			wantProfile: "work",
		},
		{
			name: "cleared values",
			commands: []Command{
				{Kind: CmdModel, Arg: "default"},
				{Kind: CmdProfile, Arg: "default"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			argsPath := filepath.Join(t.TempDir(), "args")
			t.Setenv("ARGS_FILE", argsPath)
			bin := writeFakeCake(t, `
printf '%s\n' "$@" >> "$ARGS_FILE"
`)

			m := New(Config{
				CakeBin: bin,
				Cwd:     t.TempDir(),
				Model:   "startup-model",
				Profile: "startup-profile",
			})
			m.width, m.height = 80, 24
			m.layout()
			for _, cmd := range tt.commands {
				tm, _ := m.execCommand(cmd)
				m = tm.(Model)
			}

			tm, _ := m.startRun("hello")
			m = tm.(Model)
			drainRun(t, m.run)

			raw, err := os.ReadFile(argsPath)
			if err != nil {
				t.Fatal(err)
			}
			got := string(raw)
			if tt.wantModel == "" {
				if strings.Contains(got, "--model\n") {
					t.Errorf("cleared model still reached cake: %q", got)
				}
			} else if !strings.Contains(got, "--model\n"+tt.wantModel+"\n") {
				t.Errorf("model did not reach cake: %q", got)
			}
			if tt.wantProfile == "" {
				if strings.Contains(got, "--profile\n") {
					t.Errorf("cleared profile still reached cake: %q", got)
				}
			} else if !strings.Contains(got, "--profile\n"+tt.wantProfile+"\n") {
				t.Errorf("profile did not reach cake: %q", got)
			}
		})
	}
}

func TestClearedSelectionIsOmittedFromSessionInfo(t *testing.T) {
	m := New(Config{Model: "startup-model", Profile: "startup-profile"})
	m.width, m.height = 80, 24
	m.layout()

	tm, _ := m.execCommand(Command{Kind: CmdModel, Arg: "default"})
	m = tm.(Model)
	tm, _ = m.execCommand(Command{Kind: CmdProfile, Arg: "default"})
	m = tm.(Model)
	tm, _ = m.execCommand(Command{Kind: CmdSession})
	m = tm.(Model)

	text := lastItem(t, m).Text
	if strings.Contains(text, "model:") {
		t.Errorf("session info still shows a model:\n%s", text)
	}
	if strings.Contains(text, "profile:") {
		t.Errorf("session info still shows a profile:\n%s", text)
	}
}
