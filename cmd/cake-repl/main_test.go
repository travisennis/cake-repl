package main

import (
	"bytes"
	"flag"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/travisennis/cake-repl/internal/app"
	"github.com/travisennis/cake-repl/internal/cake"
)

func TestValidateFlagsVersionShortCircuits(t *testing.T) {
	// --version should return nil even when other flags are invalid.
	resume := optionalString{set: true, value: "not-a-uuid"}
	fork := optionalString{set: true, value: "not-a-uuid"}
	if err := validateFlags(true, resume, fork, []string{"surprise!"}, "/path", true); err != nil {
		t.Errorf("validateFlags(true, …) = %v, want nil", err)
	}
}

func TestValidateFlags(t *testing.T) {
	const uuid = "11111111-2222-3333-4444-555555555555"
	tests := []struct {
		name        string
		showVersion bool
		resume      optionalString
		fork        optionalString
		args        []string
		configPath  string
		noConfig    bool
		wantErr     bool
	}{
		{
			name:    "no flags, no args",
			wantErr: false,
		},
		{
			name:    "--resume with valid uuid",
			resume:  optionalString{set: true, value: uuid},
			wantErr: false,
		},
		{
			name:    "bare --resume picks a session at startup",
			resume:  optionalString{set: true},
			wantErr: false,
		},
		{
			name:    "--fork latest",
			fork:    optionalString{set: true},
			wantErr: false,
		},
		{
			name:    "--fork with valid uuid",
			fork:    optionalString{set: true, value: uuid},
			wantErr: false,
		},
		{
			name:    "--resume with invalid uuid",
			resume:  optionalString{set: true, value: "not-a-uuid"},
			wantErr: true,
		},
		{
			name:    "--fork and --resume both set",
			fork:    optionalString{set: true},
			resume:  optionalString{set: true, value: uuid},
			wantErr: true,
		},
		{
			name:    "bare --fork and bare --resume are mutually exclusive",
			fork:    optionalString{set: true},
			resume:  optionalString{set: true},
			wantErr: true,
		},
		{
			name:    "bare --fork and --resume with a uuid are mutually exclusive",
			fork:    optionalString{set: true},
			resume:  optionalString{set: true, value: uuid},
			wantErr: true,
		},
		{
			name:    "--fork with invalid uuid",
			fork:    optionalString{set: true, value: "not-a-uuid"},
			wantErr: true,
		},
		{
			name:    "positional argument present",
			args:    []string{"unexpected"},
			wantErr: true,
		},
		{
			name:    "--resume with malformed uuid",
			resume:  optionalString{set: true, value: "11111111-2222-3333-4444-55555555555Z"}, // bad char at end
			wantErr: true,
		},
		{
			name:       "--config and --no-config both set",
			configPath: "/some/path",
			noConfig:   true,
			wantErr:    true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateFlags(tt.showVersion, tt.resume, tt.fork, tt.args, tt.configPath, tt.noConfig)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateFlags(%v, %+v, %+v, %v, %q, %v) = %v, wantErr=%v",
					tt.showVersion, tt.resume, tt.fork, tt.args, tt.configPath, tt.noConfig, err, tt.wantErr)
			}
		})
	}
}

func TestValidateFlagsVersionShortCircuitSkipsArgCheck(t *testing.T) {
	// showVersion=true must not fail even with positional arguments.
	if err := validateFlags(true, optionalString{}, optionalString{}, []string{"oops"}, "", false); err != nil {
		t.Errorf("validateFlags(true, …) with args = %v, want nil", err)
	}
}

func TestResolveCwdEmpty(t *testing.T) {
	got, err := resolveCwd("")
	if err != nil {
		t.Fatalf("resolveCwd(\"\") = _, %v", err)
	}
	want, _ := os.Getwd()
	abs, _ := filepath.Abs(want)
	if got != abs {
		t.Errorf("resolveCwd(\"\") = %q, want %q", got, abs)
	}
}

func TestResolveCwdAbsolute(t *testing.T) {
	dir := t.TempDir()
	got, err := resolveCwd(dir)
	if err != nil {
		t.Fatalf("resolveCwd(%q) = _, %v", dir, err)
	}
	if got != dir {
		t.Errorf("resolveCwd(%q) = %q, want %q", dir, got, dir)
	}
}

func TestResolveCwdRelative(t *testing.T) {
	// resolve "." and expect an absolute path back.
	got, err := resolveCwd(".")
	if err != nil {
		t.Fatalf("resolveCwd(\".\") = _, %v", err)
	}
	if !filepath.IsAbs(got) {
		t.Errorf("resolveCwd(\".\") = %q, want absolute path", got)
	}
}

func TestResolveCwdNotExist(t *testing.T) {
	_, err := resolveCwd("/tmp/cake-repl-test-nonexistent-directory-abc123")
	if err == nil {
		t.Fatal("resolveCwd with nonexistent path: want error, got nil")
	}
}

func TestResolveCwdIsFile(t *testing.T) {
	f := t.TempDir() + "/not-a-dir"
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := resolveCwd(f)
	if err == nil {
		t.Fatal("resolveCwd with file path: want error, got nil")
	}
}

// Ensure IsSessionID matches the test expectations in validateFlags.
func TestResumeUUIDIsSessionID(t *testing.T) {
	if !app.IsSessionID("11111111-2222-3333-4444-555555555555") {
		t.Error("IsSessionID rejected a valid uuid")
	}
	if app.IsSessionID("not-a-uuid") {
		t.Error("IsSessionID accepted invalid input")
	}
}

func TestInlineComposerCleanup(t *testing.T) {
	if got := inlineComposerCleanup(0); got != "" {
		t.Errorf("inlineComposerCleanup(0) = %q, want empty", got)
	}
	if got, want := inlineComposerCleanup(5), "\x1b[5A\x1b[J"; got != want {
		t.Errorf("inlineComposerCleanup(5) = %q, want %q", got, want)
	}
}

func TestNormalizeOptionalArgs(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want []string
	}{
		{name: "latest fork", args: []string{"-fork"}, want: []string{"-fork="}},
		{name: "uuid with separate value", args: []string{"--fork", "11111111-2222-3333-4444-555555555555"}, want: []string{"--fork=11111111-2222-3333-4444-555555555555"}},
		{name: "next flag means latest", args: []string{"--fork", "--no-session"}, want: []string{"--fork=", "--no-session"}},
		{name: "bare resume picks a session", args: []string{"-resume"}, want: []string{"-resume="}},
		{name: "bare double-dash resume", args: []string{"--resume"}, want: []string{"--resume="}},
		{name: "resume uuid with separate value", args: []string{"-resume", "11111111-2222-3333-4444-555555555555"}, want: []string{"-resume=11111111-2222-3333-4444-555555555555"}},
		{name: "resume next flag means picker", args: []string{"-resume", "-cwd", "/tmp"}, want: []string{"-resume=", "-cwd", "/tmp"}},
		{name: "inline values pass through", args: []string{"-resume=11111111-2222-3333-4444-555555555555", "--fork="}, want: []string{"-resume=11111111-2222-3333-4444-555555555555", "--fork="}},
		{name: "unrelated flags pass through", args: []string{"-model", "gpt-x"}, want: []string{"-model", "gpt-x"}},
		{name: "after terminator is prompt text", args: []string{"--", "--fork", "uuid", "-resume"}, want: []string{"--", "--fork", "uuid", "-resume"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeOptionalArgs(tt.args)
			if len(got) != len(tt.want) {
				t.Fatalf("normalizeOptionalArgs(%v) = %v, want %v", tt.args, got, tt.want)
			}
			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Errorf("normalizeOptionalArgs(%v)[%d] = %q, want %q", tt.args, i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestStringListFlag(t *testing.T) {
	var dirs stringList
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fs.Var(&dirs, "add-dir", "")
	if err := fs.Parse([]string{"-add-dir", "vendor", "-add-dir=/abs/path"}); err != nil {
		t.Fatalf("Parse = %v", err)
	}
	want := []string{"vendor", "/abs/path"}
	if len(dirs) != len(want) {
		t.Fatalf("got %v, want %v", dirs, want)
	}
	for i := range want {
		if dirs[i] != want[i] {
			t.Fatalf("got %v, want %v", dirs, want)
		}
	}
}

func TestResumeCommand(t *testing.T) {
	const id = "11111111-2222-3333-4444-555555555555"
	tests := []struct {
		name       string
		cfg        app.Config
		configPath string
		noConfig   bool
		explicit   map[string]bool
		want       string
	}{
		{
			name: "minimal keeps resume and cwd",
			cfg:  app.Config{Cwd: "/project"},
			want: "cake-repl -resume " + id + " -cwd /project",
		},
		{
			name: "sandbox is not dropped",
			cfg:  app.Config{Cwd: "/project", Sandbox: "read-only"},
			want: "cake-repl -resume " + id + " -cwd /project -sandbox read-only",
		},
		{
			name:       "explicit config is reproduced",
			cfg:        app.Config{Cwd: "/project"},
			configPath: "/tmp/repl.toml",
			want:       "cake-repl -resume " + id + " -config /tmp/repl.toml -cwd /project",
		},
		{
			name:     "no-config is reproduced",
			cfg:      app.Config{Cwd: "/project"},
			noConfig: true,
			want:     "cake-repl -resume " + id + " -no-config -cwd /project",
		},
		{
			name: "every session control is reproduced",
			cfg: app.Config{
				CakeBin:      "/opt/cake",
				Cwd:          "/project",
				Model:        "gpt-x",
				Profile:      "fast",
				Sandbox:      "workspace-write-interactive",
				AddDirs:      []string{"vendor", "/abs/path"},
				ToolboxDirs:  []string{".cake/tools"},
				Tools:        "bash,read",
				NoTools:      true,
				Skills:       "go,testing",
				NoSkills:     true,
				SystemPrompt: "/tmp/prompt.md",
			},
			want: "cake-repl -resume " + id + " -cake-bin /opt/cake -cwd /project -model gpt-x -profile fast" +
				" -sandbox workspace-write-interactive -add-dir vendor -add-dir /abs/path -toolbox .cake/tools" +
				" -tools bash,read -no-tools -no-skills -skills go,testing -system-prompt /tmp/prompt.md",
		},
		{
			name: "default cake bin is omitted",
			cfg:  app.Config{CakeBin: "cake", Cwd: "/project"},
			want: "cake-repl -resume " + id + " -cwd /project",
		},
		{
			name: "values with spaces are quoted",
			cfg:  app.Config{Cwd: "/my project", Sandbox: "read-only"},
			want: "cake-repl -resume " + id + " -cwd '/my project' -sandbox read-only",
		},
		{
			name:     "explicit empty model is preserved",
			cfg:      app.Config{CakeBin: "cake", Cwd: "/project"},
			explicit: map[string]bool{"model": true},
			want:     "cake-repl -resume " + id + " -cwd /project -model ''",
		},
		{
			name:     "explicit empty profile is preserved",
			cfg:      app.Config{CakeBin: "cake", Cwd: "/project"},
			explicit: map[string]bool{"profile": true},
			want:     "cake-repl -resume " + id + " -cwd /project -profile ''",
		},
		{
			name:     "explicit empty sandbox is preserved",
			cfg:      app.Config{CakeBin: "cake", Cwd: "/project"},
			explicit: map[string]bool{"sandbox": true},
			want:     "cake-repl -resume " + id + " -cwd /project -sandbox ''",
		},
		{
			name:     "explicit default cake bin is preserved",
			cfg:      app.Config{CakeBin: "cake", Cwd: "/project"},
			explicit: map[string]bool{"cake-bin": true},
			want:     "cake-repl -resume " + id + " -cake-bin cake -cwd /project",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resumeCommand(tt.cfg, tt.configPath, tt.noConfig, id, tt.explicit)
			if got != tt.want {
				t.Errorf("resumeCommand() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestShellQuote(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{in: "/plain/path-1.2:3", want: "/plain/path-1.2:3"},
		{in: "", want: "''"},
		{in: "two words", want: "'two words'"},
		{in: "it's", want: `'it'\''s'`},
		{in: "$(rm -rf)", want: "'$(rm -rf)'"},
	}
	for _, tt := range tests {
		if got := shellQuote(tt.in); got != tt.want {
			t.Errorf("shellQuote(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// splitShellFields splits a shell-quoted command line the way a POSIX shell
// would: whitespace separates fields, single quotes group a span verbatim, and a
// backslash outside quotes escapes the next byte (shellQuote puts a backslash
// between two quoted spans for an embedded quote). It only needs to understand
// the quoting shellQuote produces.
func splitShellFields(s string) []string {
	var fields []string
	var cur strings.Builder
	inQuote, started, escaped := false, false, false
	flush := func() {
		fields = append(fields, cur.String())
		cur.Reset()
		started = false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case escaped:
			cur.WriteByte(c)
			escaped, started = false, true
		case !inQuote && c == '\\':
			escaped, started = true, true
		case c == '\'':
			inQuote, started = !inQuote, true
		case !inQuote && (c == ' ' || c == '\t'):
			if started {
				flush()
			}
		default:
			cur.WriteByte(c)
			started = true
		}
	}
	if started {
		flush()
	}
	return fields
}

// hasFlag reports whether a split argv contains name as a standalone token.
func hasFlag(args []string, name string) bool {
	for _, a := range args {
		if a == name {
			return true
		}
	}
	return false
}

// hasFlagValue reports whether a split argv carries name immediately followed by
// value, matching the flag/value pairing resumeCommand emits.
func hasFlagValue(args []string, name, value string) bool {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == name && args[i+1] == value {
			return true
		}
	}
	return false
}

// flagValue pairs a flag name with a value the hint should carry.
type flagValue struct {
	name  string
	value string
}

// resumeProjection narrows an app.Config to the cake-invocation controls the
// exit hint reproduces, so a round trip compares only the fields that matter.
type resumeProjection struct {
	CakeBin      string
	Cwd          string
	Model        string
	Profile      string
	Tools        string
	NoTools      bool
	AddDirs      []string
	ToolboxDirs  []string
	Sandbox      string
	NoSkills     bool
	Skills       string
	SystemPrompt string
}

func projectResume(cfg app.Config) resumeProjection {
	return resumeProjection{
		CakeBin:      cfg.CakeBin,
		Cwd:          cfg.Cwd,
		Model:        cfg.Model,
		Profile:      cfg.Profile,
		Tools:        cfg.Tools,
		NoTools:      cfg.NoTools,
		AddDirs:      cfg.AddDirs,
		ToolboxDirs:  cfg.ToolboxDirs,
		Sandbox:      cfg.Sandbox,
		NoSkills:     cfg.NoSkills,
		Skills:       cfg.Skills,
		SystemPrompt: cfg.SystemPrompt,
	}
}

// parsedStartup is a startup argv after the real flag parse and config merge.
type parsedStartup struct {
	cfg        app.Config
	explicit   map[string]bool
	configPath string
	noConfig   bool
}

// parseStartupArgs runs argv through the same bindFlags/effectiveConfig path the
// binary uses, against whatever config files the test has laid out. It never
// spawns cake.
func parseStartupArgs(t *testing.T, argv []string) parsedStartup {
	t.Helper()
	fs := flag.NewFlagSet("cake-repl", flag.ContinueOnError)
	f := bindFlags(fs)
	if err := fs.Parse(normalizeOptionalArgs(argv)); err != nil {
		t.Fatalf("parsing %v: %v", argv, err)
	}
	explicit := explicitFlags(fs)
	cfg, err := effectiveConfig(f, explicit, io.Discard)
	if err != nil {
		t.Fatalf("effective config for %v: %v", argv, err)
	}
	return parsedStartup{cfg: cfg, explicit: explicit, configPath: *f.configPath, noConfig: *f.noConfig}
}

// TestStartupResumePicksOrPins drives -resume through the shared flag surface,
// the real flag parse, and the config merge: a UUID selects replay hydration
// plus the explicit pin, while a bare -resume asks for the startup session
// picker and hydrates nothing (ADR 021). Config files are absent, so only the
// flag decides.
func TestStartupResumePicksOrPins(t *testing.T) {
	const id = "11111111-2222-3333-4444-555555555555"
	tests := []struct {
		name     string
		args     []string
		wantMode cake.RunMode
		wantID   string
		wantPick bool
	}{
		{name: "no resume flag starts fresh", wantMode: cake.RunFresh},
		{name: "resume with a uuid hydrates and pins", args: []string{"-resume", id}, wantMode: cake.RunResume, wantID: id},
		{name: "resume with an inline uuid hydrates and pins", args: []string{"-resume=" + id}, wantMode: cake.RunResume, wantID: id},
		{name: "bare resume opens the picker", args: []string{"-resume"}, wantMode: cake.RunFresh, wantPick: true},
		{name: "bare resume before another flag opens the picker", args: []string{"-resume", "-no-color"}, wantMode: cake.RunFresh, wantPick: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			t.Setenv("XDG_CONFIG_HOME", t.TempDir())
			start := parseStartupArgs(t, tt.args)
			cfg := start.cfg
			if cfg.InitialMode != tt.wantMode || cfg.ResumeID != tt.wantID || cfg.StartupPicker != tt.wantPick {
				t.Errorf("startup cfg = (mode %v, resume %q, picker %v), want (%v, %q, %v)",
					cfg.InitialMode, cfg.ResumeID, cfg.StartupPicker, tt.wantMode, tt.wantID, tt.wantPick)
			}
		})
	}
}

// TestResumeCommandRoundTrip drives the exit hint through the whole startup path:
// a startup argv becomes an effective config, the hint is rendered from it, and
// the hint is split and parsed back into an effective config. A pasted resume
// must reproduce the original run's cake invocation, including explicit overrides
// whose effective value is empty (-model "", -sandbox "") or the default
// (-cake-bin cake). Config files are fake and local; no real cake is spawned.
func TestResumeCommandRoundTrip(t *testing.T) {
	const id = "11111111-2222-3333-4444-555555555555"

	// Model/profile/cake-bin all come from a config file, so an explicit override
	// has something to suppress on the reload.
	const xdgConfig = "model = \"xdg-model\"\nprofile = \"xdg-profile\"\ncake-bin = \"/xdg/cake\"\n"
	const localConfig = "model = \"local-model\"\nprofile = \"local-profile\"\n"
	// Selected only by an explicit -config, so it may also carry cake-bin.
	const explicitConfig = "model = \"explicit-model\"\nprofile = \"explicit-profile\"\ncake-bin = \"./explicit-cake\"\n"

	tests := []struct {
		name       string
		args       []string
		wantArgs   []flagValue
		wantFlags  []string
		wantAbsent []string
		wantModel  string
		wantProf   string
		wantSand   string
		wantBin    string
	}{
		{
			name:      "plain run keeps config-derived values",
			wantModel: "local-model",
			wantProf:  "local-profile",
			wantSand:  defaultSandbox,
			wantBin:   "/xdg/cake",
		},
		{
			name:      "explicit model and profile clears beat config",
			args:      []string{"-model", "", "-profile", ""},
			wantArgs:  []flagValue{{"-model", ""}, {"-profile", ""}},
			wantModel: "",
			wantProf:  "",
			wantSand:  defaultSandbox,
			wantBin:   "/xdg/cake",
		},
		{
			name:      "explicit default cake bin beats config",
			args:      []string{"-cake-bin", "cake"},
			wantArgs:  []flagValue{{"-cake-bin", "cake"}},
			wantModel: "local-model",
			wantProf:  "local-profile",
			wantSand:  defaultSandbox,
			wantBin:   "cake",
		},
		{
			name:      "explicit empty sandbox survives",
			args:      []string{"-sandbox", ""},
			wantArgs:  []flagValue{{"-sandbox", ""}},
			wantModel: "local-model",
			wantProf:  "local-profile",
			wantSand:  "",
			wantBin:   "/xdg/cake",
		},
		{
			name:      "explicit non-default sandbox survives",
			args:      []string{"-sandbox", "read-only"},
			wantArgs:  []flagValue{{"-sandbox", "read-only"}},
			wantModel: "local-model",
			wantProf:  "local-profile",
			wantSand:  "read-only",
			wantBin:   "/xdg/cake",
		},
		{
			name: "every session control round trips through quoting",
			args: []string{
				"-model", "cli-model", "-profile", "cli-profile",
				"-sandbox", "danger-full-access",
				"-add-dir", "vendor", "-add-dir", "/abs dir",
				"-toolbox", ".cake/tools", "-tools", "bash,read",
				"-no-tools", "-no-skills", "-skills", "go,testing",
				"-system-prompt", "/tmp/it's prompt.md",
			},
			wantArgs:  []flagValue{{"-add-dir", "/abs dir"}, {"-system-prompt", "/tmp/it's prompt.md"}},
			wantModel: "cli-model",
			wantProf:  "cli-profile",
			wantSand:  "danger-full-access",
			wantBin:   "/xdg/cake",
		},
		{
			name:      "explicit config file round trips",
			args:      []string{"-config", "explicit.toml"},
			wantArgs:  []flagValue{{"-config", "explicit.toml"}},
			wantModel: "explicit-model",
			wantProf:  "explicit-profile",
			wantSand:  defaultSandbox,
			wantBin:   "./explicit-cake",
		},
		{
			name:       "no-config skips config values",
			args:       []string{"-no-config"},
			wantFlags:  []string{"-no-config"},
			wantAbsent: []string{"-config"},
			wantModel:  "",
			wantProf:   "",
			wantSand:   defaultSandbox,
			wantBin:    "cake",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			t.Chdir(dir)
			t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "xdg"))
			writeFile := func(path, content string) {
				t.Helper()
				if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			writeFile(filepath.Join(dir, "xdg", "cake-repl", "config.toml"), xdgConfig)
			writeFile(filepath.Join(dir, ".cake-repl.toml"), localConfig)
			writeFile(filepath.Join(dir, "explicit.toml"), explicitConfig)

			// -cwd points at a directory with a space so the hint has to quote it.
			spaceDir := filepath.Join(dir, "my project")
			if err := os.MkdirAll(spaceDir, 0o755); err != nil {
				t.Fatal(err)
			}
			args := append(append([]string{}, tt.args...), "-cwd", spaceDir)

			start := parseStartupArgs(t, args)
			hint := resumeCommand(start.cfg, start.configPath, start.noConfig, id, start.explicit)

			fields := splitShellFields(hint)
			if len(fields) < 3 || fields[0] != "cake-repl" || fields[1] != "-resume" || fields[2] != id {
				t.Fatalf("hint = %q, want it to lead with %q", hint, "cake-repl -resume "+id)
			}
			for _, fv := range tt.wantArgs {
				if !hasFlagValue(fields, fv.name, fv.value) {
					t.Errorf("hint = %q, want it to carry %q %q", hint, fv.name, fv.value)
				}
			}
			for _, name := range tt.wantFlags {
				if !hasFlag(fields, name) {
					t.Errorf("hint = %q, want it to carry %q", hint, name)
				}
			}
			for _, name := range tt.wantAbsent {
				if hasFlag(fields, name) {
					t.Errorf("hint = %q, want it to omit %q", hint, name)
				}
			}

			resumed := parseStartupArgs(t, fields[1:])
			if got, want := projectResume(resumed.cfg), projectResume(start.cfg); !reflect.DeepEqual(got, want) {
				t.Errorf("resumed controls = %+v, want %+v", got, want)
			}
			if resumed.cfg.InitialMode != cake.RunResume || resumed.cfg.ResumeID != id {
				t.Errorf("resumed run mode = (%v, %q), want (%v, %q)",
					resumed.cfg.InitialMode, resumed.cfg.ResumeID, cake.RunResume, id)
			}
			got := resumed.cfg
			if got.Model != tt.wantModel || got.Profile != tt.wantProf || got.Sandbox != tt.wantSand || got.CakeBin != tt.wantBin {
				t.Errorf("resumed (model, profile, sandbox, cake-bin) = (%q, %q, %q, %q), want (%q, %q, %q, %q)",
					got.Model, got.Profile, got.Sandbox, got.CakeBin, tt.wantModel, tt.wantProf, tt.wantSand, tt.wantBin)
			}
		})
	}
}

func TestStartupConfigExecutablePrecedence(t *testing.T) {
	tests := []struct {
		name, xdg, local, cli, want    string
		explicitConfig, noConfig, warn bool
	}{
		{name: "local cannot override XDG", xdg: `cake-bin = "../trusted-cake"`, local: `cake-bin = "./repo-cake"`, want: "../trusted-cake", warn: true},
		{name: "local cannot override default", local: `cake-bin = "./repo-cake"`, want: "cake", warn: true},
		{name: "empty local key warns", local: `cake-bin = ""`, want: "cake", warn: true},
		{name: "local value controls never reach warning", local: `cake-bin = "\u001b[2J\u001b]52;c;payload\u0007"`, want: "cake", warn: true},
		{name: "explicit local file is trusted", xdg: `cake-bin = "../trusted-cake"`, local: `cake-bin = "./repo-cake"`, explicitConfig: true, want: "./repo-cake"},
		{name: "CLI overrides default layers", xdg: `cake-bin = "../trusted-cake"`, local: `cake-bin = "./repo-cake"`, cli: "./cli-cake", want: "./cli-cake", warn: true},
		{name: "CLI overrides explicit file", local: `cake-bin = "./repo-cake"`, explicitConfig: true, cli: "./cli-cake", want: "./cli-cake"},
		{name: "no-config skips values and warnings", xdg: `cake-bin = "../trusted-cake"`, local: `cake-bin = "./repo-cake"`, noConfig: true, want: "cake"},
		{name: "no-config skips malformed files", xdg: `cake-bin = `, local: `cake-bin = `, noConfig: true, want: "cake"},
		{name: "no-config keeps CLI", local: `cake-bin = "./repo-cake"`, noConfig: true, cli: "./cli-cake", want: "./cli-cake"},
		{name: "absent key is quiet", xdg: `cake-bin = "../trusted-cake"`, want: "../trusted-cake"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			t.Chdir(dir)
			t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "xdg"))
			xdgPath := filepath.Join(dir, "xdg", "cake-repl", "config.toml")
			if err := os.MkdirAll(filepath.Dir(xdgPath), 0o700); err != nil {
				t.Fatal(err)
			}
			for path, content := range map[string]string{xdgPath: tt.xdg, ".cake-repl.toml": tt.local} {
				if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			var warnings bytes.Buffer
			fileCfg, err := loadStartupConfig(".cake-repl.toml", tt.explicitConfig, tt.noConfig, &warnings)
			if err != nil {
				t.Fatal(err)
			}
			cfg := app.Config{CakeBin: "cake"}
			explicit := map[string]bool{}
			if tt.cli != "" {
				cfg.CakeBin = tt.cli
				explicit["cake-bin"] = true
			}
			applyConfigFile(&cfg, fileCfg, explicit)
			if cfg.CakeBin != tt.want {
				t.Fatalf("CakeBin = %q, want %q", cfg.CakeBin, tt.want)
			}
			wantWarning := ""
			if tt.warn {
				wantWarning = "cake-repl: warning: ignoring cake-bin in project-local config \".cake-repl.toml\"; use XDG config, -config, or -cake-bin to select the executable\n"
			}
			if warnings.String() != wantWarning {
				t.Fatalf("warning = %q, want %q", warnings.String(), wantWarning)
			}
		})
	}
}

func TestStartupConfigPreservesOtherLocalDefaults(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if err := os.WriteFile(".cake-repl.toml", []byte(`
cake-bin = "./repo-cake"
model = "local-model"
profile = "local-profile"
output-limit = 5000
max-timeline-items = 200
tool-color = true
`), 0o600); err != nil {
		t.Fatal(err)
	}
	var warnings bytes.Buffer
	fileCfg, err := loadStartupConfig("", false, false, &warnings)
	if err != nil {
		t.Fatal(err)
	}
	cfg := app.Config{CakeBin: "cake", Model: "cli-model"}
	applyConfigFile(&cfg, fileCfg, map[string]bool{"model": true})
	if cfg.CakeBin != "cake" || cfg.Model != "cli-model" || cfg.Profile != "local-profile" || cfg.OutputLimit != 5000 || cfg.MaxTimelineItems != 200 || !cfg.ToolColor {
		t.Fatalf("effective defaults = %+v", cfg)
	}
}

// TestStartupConfigToolColorPrecedence checks the effective app.Config that
// startup builds from real XDG and project-local files. tool-color must honor
// the documented project-over-XDG precedence in both directions (including an
// explicit false), an absent key must fall back to the hardcoded default, and
// an explicit CLI -tool-color must win over either file layer.
func TestStartupConfigToolColorPrecedence(t *testing.T) {
	tests := []struct {
		name     string
		xdg      string
		local    string
		cliSet   bool
		cliValue bool
		want     bool
	}{
		{name: "project false overrides XDG true", xdg: "tool-color = true\n", local: "tool-color = false\n", want: false},
		{name: "absent project key inherits XDG true", xdg: "tool-color = true\n", local: "model = \"local\"\n", want: true},
		{name: "project true overrides XDG false", xdg: "tool-color = false\n", local: "tool-color = true\n", want: true},
		{name: "absent key falls back to default off", xdg: "", local: "model = \"local\"\n", want: false},
		{name: "CLI false beats XDG true", xdg: "tool-color = true\n", local: "", cliSet: true, cliValue: false, want: false},
		{name: "CLI false beats project true", xdg: "", local: "tool-color = true\n", cliSet: true, cliValue: false, want: false},
		{name: "CLI true beats project false", xdg: "", local: "tool-color = false\n", cliSet: true, cliValue: true, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			t.Chdir(dir)
			t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "xdg"))
			xdgPath := filepath.Join(dir, "xdg", "cake-repl", "config.toml")
			if err := os.MkdirAll(filepath.Dir(xdgPath), 0o700); err != nil {
				t.Fatal(err)
			}
			for path, content := range map[string]string{xdgPath: tt.xdg, ".cake-repl.toml": tt.local} {
				if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			var warnings bytes.Buffer
			fileCfg, err := loadStartupConfig("", false, false, &warnings)
			if err != nil {
				t.Fatal(err)
			}
			cfg := app.Config{}
			explicit := map[string]bool{}
			if tt.cliSet {
				cfg.ToolColor = tt.cliValue
				explicit["tool-color"] = true
			}
			applyConfigFile(&cfg, fileCfg, explicit)
			if cfg.ToolColor != tt.want {
				t.Fatalf("effective ToolColor = %v, want %v", cfg.ToolColor, tt.want)
			}
		})
	}
}

func TestStartupConfigMalformedFiles(t *testing.T) {
	for _, layer := range []string{"XDG", "project-local", "explicit"} {
		t.Run(layer, func(t *testing.T) {
			t.Chdir(t.TempDir())
			t.Setenv("XDG_CONFIG_HOME", t.TempDir())
			path := ".cake-repl.toml"
			if layer == "XDG" {
				path = filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "cake-repl", "config.toml")
				if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(path, []byte(`cake-bin = `), 0o600); err != nil {
				t.Fatal(err)
			}
			var warnings bytes.Buffer
			_, err := loadStartupConfig(path, layer == "explicit", false, &warnings)
			if err == nil || !strings.Contains(err.Error(), "loading") {
				t.Fatalf("malformed %s config error = %v", layer, err)
			}
		})
	}
}

// TestStartupCompactionConfig drives the `[compaction]` table through the real
// config layers and flag surface: disabled by default, XDG-tunable, unable to be
// enabled or pointed at an executable by the untrusted project-local layer,
// disabled by -no-session, and rejected when the threshold is impossible.
func TestStartupCompactionConfig(t *testing.T) {
	defaults := app.CompactionConfig{
		Bin:              "cake-compact",
		ThresholdPercent: 80,
		KeepTokens:       20000,
	}
	enabled := defaults
	enabled.Enabled = true

	const enableXDG = "[compaction]\nenabled = true\nthreshold-percent = 70\nkeep-tokens = 12000\n"
	enabledTuned := enabled
	enabledTuned.ThresholdPercent = 70
	enabledTuned.KeepTokens = 12000
	disabledTuned := enabledTuned
	disabledTuned.Enabled = false

	const absoluteXDG = "[compaction]\nenabled = true\nmax-context-tokens = 400000\ntrigger-tokens = 350000\nkeep-tokens = 12000\n"
	absoluteTuned := enabled
	absoluteTuned.MaxContextTokens = 400000
	absoluteTuned.TriggerTokens = 350000
	absoluteTuned.KeepTokens = 12000

	const selectionXDG = "[compaction]\nenabled = true\nmodel = \"summary-model\"\nprofile = \"summary-profile\"\nreasoning-effort = \"low\"\n"
	selectionTuned := enabled
	selectionTuned.Model = "summary-model"
	selectionTuned.Profile = "summary-profile"
	selectionTuned.ReasoningEffort = "low"

	tests := []struct {
		name          string
		xdg           string
		local         string
		args          []string
		want          app.CompactionConfig
		wantWarn      []string
		wantWarnExact string
		wantErr       string
	}{
		{name: "disabled by default", want: defaults},
		{name: "XDG enables and tunes", xdg: enableXDG, want: enabledTuned},
		{name: "XDG enables absolute mode", xdg: absoluteXDG, want: absoluteTuned},
		{name: "XDG selects the compaction summary model", xdg: selectionXDG, want: selectionTuned},
		{
			name:  "project cannot select the compaction summary model",
			xdg:   selectionXDG,
			local: "[compaction]\nmodel = \"untrusted-model\"\nprofile = \"untrusted-profile\"\nreasoning-effort = \"max\"\n",
			want:  selectionTuned,
			wantWarn: []string{
				"ignoring compaction.model",
				"ignoring compaction.profile",
				"ignoring compaction.reasoning-effort",
			},
		},
		{
			name:     "project cannot tune absolute thresholds",
			xdg:      absoluteXDG,
			local:    "[compaction]\nmax-context-tokens = 900000\ntrigger-tokens = 1\n",
			want:     absoluteTuned,
			wantWarn: []string{"ignoring compaction.max-context-tokens", "ignoring compaction.trigger-tokens"},
		},
		{
			name:    "invalid absolute trigger is rejected",
			xdg:     "[compaction]\nenabled = true\nmax-context-tokens = 400000\ntrigger-tokens = 400000\n",
			wantErr: "trigger-tokens",
		},
		{
			name:    "partial absolute config is rejected",
			xdg:     "[compaction]\nenabled = true\nmax-context-tokens = 400000\n",
			wantErr: "must be set together",
		},
		{
			name:     "project cannot enable or select the executable",
			local:    "[compaction]\nenabled = true\ncake-compact-bin = \"/tmp/untrusted\"\n",
			want:     defaults,
			wantWarn: []string{"ignoring compaction.cake-compact-bin", "ignoring compaction.enabled = true"},
		},
		{
			name:     "project can disable while tuning is ignored",
			xdg:      enableXDG,
			local:    "[compaction]\nenabled = false\nthreshold-percent = 60\n",
			want:     disabledTuned,
			wantWarn: []string{"ignoring compaction.threshold-percent"},
		},
		{
			name:     "project cannot tune thresholds",
			xdg:      enableXDG,
			local:    "[compaction]\nthreshold-percent = 1\nkeep-tokens = 1\n",
			want:     enabledTuned,
			wantWarn: []string{"ignoring compaction.threshold-percent", "ignoring compaction.keep-tokens"},
			wantWarnExact: "cake-repl: warning: ignoring compaction.threshold-percent in project-local config \".cake-repl.toml\"; tune compaction in XDG config or an explicit -config\n" +
				"cake-repl: warning: ignoring compaction.keep-tokens in project-local config \".cake-repl.toml\"; tune compaction in XDG config or an explicit -config\n",
		},
		{
			name:     "no-session disables with a warning",
			xdg:      enableXDG,
			args:     []string{"-no-session"},
			want:     disabledTuned,
			wantWarn: []string{"disabled by -no-session"},
		},
		{
			name:    "invalid threshold is rejected",
			xdg:     "[compaction]\nenabled = true\nthreshold-percent = 101\n",
			wantErr: "threshold-percent",
		},
		{
			name:    "invalid reasoning effort is rejected",
			xdg:     "[compaction]\nenabled = true\nreasoning-effort = \"maximal\"\n",
			wantErr: "reasoning-effort",
		},
		{
			name:     "invalid project tuning is ignored",
			xdg:      enableXDG,
			local:    "[compaction]\nthreshold-percent = -1\n",
			want:     enabledTuned,
			wantWarn: []string{"ignoring compaction.threshold-percent"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			t.Chdir(dir)
			t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "xdg"))
			xdgPath := filepath.Join(dir, "xdg", "cake-repl", "config.toml")
			if err := os.MkdirAll(filepath.Dir(xdgPath), 0o700); err != nil {
				t.Fatal(err)
			}
			for path, content := range map[string]string{xdgPath: tt.xdg, ".cake-repl.toml": tt.local} {
				if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
					t.Fatal(err)
				}
			}

			fs := flag.NewFlagSet("cake-repl", flag.ContinueOnError)
			f := bindFlags(fs)
			if err := fs.Parse(normalizeOptionalArgs(tt.args)); err != nil {
				t.Fatalf("parsing %v: %v", tt.args, err)
			}
			var warnings bytes.Buffer
			cfg, err := effectiveConfig(f, explicitFlags(fs), &warnings)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %v, want it to mention %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if cfg.Compaction != tt.want {
				t.Errorf("compaction = %+v, want %+v", cfg.Compaction, tt.want)
			}
			for _, want := range tt.wantWarn {
				if !strings.Contains(warnings.String(), want) {
					t.Errorf("warnings = %q, want it to contain %q", warnings.String(), want)
				}
			}
			if tt.wantWarnExact != "" && warnings.String() != tt.wantWarnExact {
				t.Errorf("warnings = %q, want exactly %q", warnings.String(), tt.wantWarnExact)
			}
			if tt.wantWarn == nil && warnings.Len() != 0 {
				t.Errorf("unexpected warnings = %q", warnings.String())
			}
		})
	}
}

// TestStartupSuggestionsConfig drives the `[suggestions]` table through the
// real config layers and flag surface: disabled by default, XDG-tunable,
// unable to be enabled or pointed at an executable by the untrusted
// project-local layer, and disabled by -no-session.
func TestStartupSuggestionsConfig(t *testing.T) {
	defaults := app.SuggestionConfig{Bin: "cake-suggest"}
	enabled := defaults
	enabled.Enabled = true

	tests := []struct {
		name          string
		xdg           string
		local         string
		args          []string
		want          app.SuggestionConfig
		wantWarn      []string
		wantWarnExact string
	}{
		{name: "disabled by default", want: defaults},
		{
			name: "XDG enables, selects the executable, and selects a suggestion model",
			xdg:  "[suggestions]\nenabled = true\ncake-suggest-bin = \"/opt/cake-suggest\"\nmodel = \"suggestion-model\"\nprofile = \"suggestion-profile\"\n",
			want: app.SuggestionConfig{Enabled: true, Bin: "/opt/cake-suggest", Model: "suggestion-model", Profile: "suggestion-profile"},
		},
		{
			name:     "project cannot enable or select any suggester setting",
			local:    "[suggestions]\nenabled = true\ncake-suggest-bin = \"/tmp/untrusted\"\nmodel = \"untrusted-model\"\nprofile = \"untrusted-profile\"\n",
			want:     defaults,
			wantWarn: []string{"ignoring suggestions.cake-suggest-bin", "ignoring suggestions.model", "ignoring suggestions.profile", "ignoring suggestions.enabled = true"},
			wantWarnExact: "cake-repl: warning: ignoring suggestions.cake-suggest-bin in project-local config \".cake-repl.toml\"; use XDG config or -config to select the suggester\n" +
				"cake-repl: warning: ignoring suggestions.model in project-local config \".cake-repl.toml\"; tune suggestions in XDG config or an explicit -config\n" +
				"cake-repl: warning: ignoring suggestions.profile in project-local config \".cake-repl.toml\"; tune suggestions in XDG config or an explicit -config\n" +
				"cake-repl: warning: ignoring suggestions.enabled = true in project-local config \".cake-repl.toml\"; enable suggestions in XDG config or an explicit -config\n",
		},
		{
			name:  "project can disable an enabled suggestion",
			xdg:   "[suggestions]\nenabled = true\n",
			local: "[suggestions]\nenabled = false\n",
			want:  defaults,
		},
		{
			name:     "no-session disables with a warning",
			xdg:      "[suggestions]\nenabled = true\n",
			args:     []string{"-no-session"},
			want:     defaults,
			wantWarn: []string{"prompt suggestions are disabled by -no-session"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			t.Chdir(dir)
			t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "xdg"))
			xdgPath := filepath.Join(dir, "xdg", "cake-repl", "config.toml")
			if err := os.MkdirAll(filepath.Dir(xdgPath), 0o700); err != nil {
				t.Fatal(err)
			}
			for path, content := range map[string]string{xdgPath: tt.xdg, ".cake-repl.toml": tt.local} {
				if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
					t.Fatal(err)
				}
			}

			fs := flag.NewFlagSet("cake-repl", flag.ContinueOnError)
			f := bindFlags(fs)
			if err := fs.Parse(normalizeOptionalArgs(tt.args)); err != nil {
				t.Fatalf("parsing %v: %v", tt.args, err)
			}
			var warnings bytes.Buffer
			cfg, err := effectiveConfig(f, explicitFlags(fs), &warnings)
			if err != nil {
				t.Fatal(err)
			}
			if cfg.Suggestions != tt.want {
				t.Errorf("suggestions = %+v, want %+v", cfg.Suggestions, tt.want)
			}
			for _, want := range tt.wantWarn {
				if !strings.Contains(warnings.String(), want) {
					t.Errorf("warnings = %q, want it to contain %q", warnings.String(), want)
				}
			}
			if tt.wantWarnExact != "" && warnings.String() != tt.wantWarnExact {
				t.Errorf("warnings = %q, want exactly %q", warnings.String(), tt.wantWarnExact)
			}
			if tt.wantWarn == nil && warnings.Len() != 0 {
				t.Errorf("unexpected warnings = %q", warnings.String())
			}
		})
	}
}
