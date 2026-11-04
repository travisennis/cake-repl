package app

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/travisennis/cake-repl/internal/cake"
	"github.com/travisennis/cake-repl/internal/childproc"
	"github.com/travisennis/cake-repl/internal/ui"
)

// clearTrustEnv keeps a test independent of the developer's cake environment so
// the startup probe is not accidentally suppressed.
func clearTrustEnv(t *testing.T) {
	t.Helper()
	t.Setenv("CAKE_TRUST_PROJECT", "")
	t.Setenv("CAKE_PROJECT_CONFIG", "")
}

// runStartupTrustProbe runs the commands Init issued until the trust probe
// returns, then applies its result the way the Bubble Tea loop would. It fails
// when Init did not issue a probe, so wiring and behavior stay covered together.
func runStartupTrustProbe(t *testing.T, m Model) Model {
	t.Helper()
	msg := m.Init()()
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		t.Fatalf("Init message type = %T, want tea.BatchMsg", msg)
	}
	for _, cmd := range batch {
		if cmd == nil {
			continue
		}
		res, ok := cmd().(trustStatusMsg)
		if !ok {
			continue
		}
		tm, _ := m.Update(res)
		return tm.(Model)
	}
	t.Fatal("Init issued no trust probe")
	return m
}

func trustStatusDocument(state string) string {
	return `{"schema_version":1,"command":"trust status","status":"ok","summary":{"state":"` + state + `"},"checks":[],"data":{` +
		`"state":"` + state + `","project_root":"/tmp/project","authority":[` +
		`{"path":".cake/settings.toml","kind":"settings","keys":["system_prompt"]}],` +
		`"changes":[]}}`
}

// TestStartupTrustProbeWarnsAtStartup is the core behavior: once a prompt-run
// invocation would be blocked, the startup warning says so as soon as the probe
// returns, and it names the fix. See ADR 035.
func TestStartupTrustProbeWarnsAtStartup(t *testing.T) {
	clearTrustEnv(t)
	bin := writeFakeCake(t, `
if [ "$1" = "trust" ]; then
cat <<'EOF'
`+trustStatusDocument("untrusted")+`
EOF
exit 0
fi
exit 9
`)
	m := laidOut(100, 24)
	m.cfg.CakeBin = bin
	m.cfg.Cwd = t.TempDir()

	m = runStartupTrustProbe(t, m)

	if m.running {
		t.Fatal("the startup trust probe started a run")
	}
	it := lastItem(t, m)
	if it.Kind != ui.KindWarning {
		t.Fatalf("last startup result = %+v, want a warning", it)
	}
	for _, want := range []string{
		"project configuration is untrusted",
		".cake/settings.toml",
		"system_prompt",
		"/tmp/project",
		"Run cake trust",
		"exit 3",
	} {
		if !strings.Contains(it.Text, want) {
			t.Errorf("warning %q does not contain %q", it.Text, want)
		}
	}
	if view := strings.Join(m.timeline.rendered, "\n\n"); !strings.Contains(view, "project configuration is untrusted") {
		t.Errorf("warning did not reach the rendered timeline:\n%s", view)
	}
}

// TestStartupTrustProbeSilentWhenUsable: trusted and not-required snapshots add
// no timeline item and say nothing.
func TestStartupTrustProbeSilentWhenUsable(t *testing.T) {
	for _, state := range []string{"trusted", "not_required"} {
		t.Run(state, func(t *testing.T) {
			clearTrustEnv(t)
			bin := writeFakeCake(t, `
if [ "$1" = "trust" ]; then
cat <<'EOF'
`+trustStatusDocument(state)+`
EOF
exit 0
fi
exit 9
`)
			m := laidOut(80, 24)
			m.cfg.CakeBin = bin
			m.cfg.Cwd = t.TempDir()
			before := len(m.timeline.items)

			m = runStartupTrustProbe(t, m)

			if got := len(m.timeline.items); got != before {
				t.Fatalf("timeline grew from %d to %d items for state %q", before, got, state)
			}
			if it := lastItem(t, m); it.Kind == ui.KindWarning {
				t.Errorf("state %q produced a warning: %+v", state, it)
			}
		})
	}
}

// TestStartupTrustProbeFailureIsSilent: a cake without trust status, malformed
// output, or a non-zero exit leaves the timeline untouched; the debug log is
// the only observer.
func TestStartupTrustProbeFailureIsSilent(t *testing.T) {
	tests := []struct {
		name   string
		script string
	}{
		{"malformed", "echo 'not json'\nexit 0\n"},
		{"nonzero", "echo 'trust status broke' >&2\nexit 2\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clearTrustEnv(t)
			bin := writeFakeCake(t, `
if [ "$1" = "trust" ]; then
`+tt.script+`fi
exit 9
`)
			var log bytes.Buffer
			m := laidOut(80, 24)
			m.cfg.CakeBin = bin
			m.cfg.Cwd = t.TempDir()
			m.cfg.DebugLog = &log
			before := len(m.timeline.items)

			m = runStartupTrustProbe(t, m)

			if got := len(m.timeline.items); got != before {
				t.Fatalf("probe failure changed the timeline from %d to %d items", before, got)
			}
			if !strings.Contains(log.String(), "trust status probe") {
				t.Errorf("probe failure was not written to the debug log: %q", log.String())
			}
		})
	}
}

func TestStartupTrustProbeUnknownStateWarns(t *testing.T) {
	clearTrustEnv(t)
	bin := writeFakeCake(t, `
if [ "$1" = "trust" ]; then
echo '{"schema_version":2,"data":{"state":"expired","project_root":"/tmp/project","authority":[],"changes":[]}}'
exit 0
fi
exit 9
`)
	m := laidOut(80, 24)
	m.cfg.CakeBin = bin
	m.cfg.Cwd = t.TempDir()

	m = runStartupTrustProbe(t, m)

	it := lastItem(t, m)
	if it.Kind != ui.KindWarning {
		t.Fatalf("unknown state produced %+v, want a warning", it)
	}
	if !strings.Contains(it.Text, `"expired"`) {
		t.Errorf("warning = %q, want the unrecognized state named", it.Text)
	}
}

func TestTrustWarningStates(t *testing.T) {
	base := cake.TrustStatus{
		ProjectRoot: "/repo",
		Authority: []cake.TrustAuthority{
			{Path: ".cake/settings.toml", Keys: []string{"system_prompt", "directories"}},
		},
	}
	tests := []struct {
		name   string
		status cake.TrustStatus
		want   []string
	}{
		{
			name:   "untrusted",
			status: withTrustState(base, cake.TrustStateUntrusted),
			want: []string{
				"is untrusted", "1 authority-bearing file",
				".cake/settings.toml: system_prompt, directories",
				"/repo", "Run cake trust",
			},
		},
		{
			name: "changed",
			status: func() cake.TrustStatus {
				s := withTrustState(base, cake.TrustStateChanged)
				s.Changes = []cake.TrustChange{{Path: ".cake/settings.toml", Change: "modified"}}
				return s
			}(),
			want: []string{
				"changed since it was trusted", "1 changed file",
				".cake/settings.toml: modified", "/repo", "Run cake trust",
			},
		},
		{
			name: "invalid",
			status: func() cake.TrustStatus {
				s := withTrustState(base, cake.TrustStateInvalid)
				s.InvalidReason = "the trust record is a symlink"
				return s
			}(),
			want: []string{
				"trust is invalid", "1 authority-bearing file",
				".cake/settings.toml: system_prompt, directories",
				"the trust record is a symlink",
				"/repo", "Run cake trust",
			},
		},
		{
			name:   "unknown",
			status: withTrustState(base, cake.TrustState("expired")),
			want: []string{
				`trust state is "expired"`, "1 authority-bearing file",
				"/repo", "cake trust --status",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := trustWarning(tt.status)
			for _, want := range tt.want {
				if !strings.Contains(got, want) {
					t.Errorf("warning %q does not contain %q", got, want)
				}
			}
		})
	}
}

func withTrustState(status cake.TrustStatus, state cake.TrustState) cake.TrustStatus {
	status.State = state
	return status
}

func TestTrustAuthoritySummaryCapsLongLists(t *testing.T) {
	status := cake.TrustStatus{Authority: []cake.TrustAuthority{
		{Path: ".cake/settings.toml", Keys: []string{"system_prompt"}},
		{Path: ".cake/hooks.json"},
		{Path: ".cake/tools/one"},
	}}
	got := trustAuthoritySummary(status)
	for _, want := range []string{"3 authority-bearing files", ".cake/settings.toml: system_prompt", ".cake/hooks.json", "… and 1 more"} {
		if !strings.Contains(got, want) {
			t.Errorf("summary %q does not contain %q", got, want)
		}
	}
	if strings.Contains(got, ".cake/tools/one") {
		t.Errorf("summary %q should cap the file list", got)
	}
}

func TestTrustProbeSkippedWhenNoEngineOrTrustEnvOverride(t *testing.T) {
	clearTrustEnv(t)
	if cmd := startTrustProbeCmd(Config{}); cmd != nil {
		t.Error("a model with no cake binary should not probe")
	}
	for _, tc := range []struct {
		name, value string
	}{
		{"CAKE_TRUST_PROJECT", "1"},
		{"CAKE_PROJECT_CONFIG", "off"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			clearTrustEnv(t)
			t.Setenv(tc.name, tc.value)
			if cmd := startTrustProbeCmd(Config{CakeBin: "cake"}); cmd != nil {
				t.Errorf("%s=%s should suppress the trust probe", tc.name, tc.value)
			}
		})
	}
}

// TestTrustProbeRegistersForShutdown proves the startup probe joins the
// process-level registry, so quitting cancels and reaps a helper that is still
// reading project state.
func TestTrustProbeRegistersForShutdown(t *testing.T) {
	clearTrustEnv(t)
	started := filepath.Join(t.TempDir(), "started")
	t.Setenv("STARTED_FILE", started)
	bin := writeFakeCake(t, `
if [ "$1" = "trust" ]; then
touch "$STARTED_FILE"
exec sleep 30
fi
exit 9
`)
	children := childproc.NewRegistry()
	t.Cleanup(func() { _ = children.Shutdown(time.Second) })
	m := New(Config{CakeBin: bin, Cwd: t.TempDir(), Children: children})
	cmd := startTrustProbeCmd(m.cfg)
	if cmd == nil {
		t.Fatal("trust probe was not created")
	}
	result := make(chan tea.Msg, 1)
	go func() { result <- cmd() }()
	waitForFile(t, started)

	start := time.Now()
	if err := m.CancelRunning(); err != nil {
		t.Fatalf("CancelRunning: %v", err)
	}
	if elapsed := time.Since(start); elapsed > ChildTeardownTimeout {
		t.Errorf("CancelRunning took %s, want within %s", elapsed, ChildTeardownTimeout)
	}
	select {
	case msg := <-result:
		res, ok := msg.(trustStatusMsg)
		if !ok {
			t.Fatalf("probe returned %T, want trustStatusMsg", msg)
		}
		if res.err == nil {
			t.Error("canceled probe reported success, want an error")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("canceled probe never returned")
	}
}

func TestTrustProbeEnvOverrideRequiresExactCakeValues(t *testing.T) {
	tests := []struct {
		name, value string
		want        bool
	}{
		{"CAKE_TRUST_PROJECT", "1", true},
		{"CAKE_TRUST_PROJECT", "true", false},
		{"CAKE_TRUST_PROJECT", "0", false},
		{"CAKE_PROJECT_CONFIG", "off", true},
		{"CAKE_PROJECT_CONFIG", "false", false},
	}
	for _, tt := range tests {
		t.Run(tt.name+"="+tt.value, func(t *testing.T) {
			clearTrustEnv(t)
			t.Setenv(tt.name, tt.value)
			if got := projectTrustOverridden(); got != tt.want {
				t.Errorf("projectTrustOverridden = %v, want %v", got, tt.want)
			}
		})
	}
}
