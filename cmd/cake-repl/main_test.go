package main

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/travisennis/cake-repl/internal/app"
)

func TestValidateFlagsVersionShortCircuits(t *testing.T) {
	// --version should return nil even when other flags are invalid.
	if err := validateFlags(true, "not-a-uuid", true, "not-a-uuid", []string{"surprise!"}, "/path", true); err != nil {
		t.Errorf("validateFlags(true, …) = %v, want nil", err)
	}
}

func TestValidateFlags(t *testing.T) {
	tests := []struct {
		name        string
		showVersion bool
		resume      string
		fork        bool
		forkID      string
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
			resume:  "11111111-2222-3333-4444-555555555555",
			wantErr: false,
		},
		{
			name:    "--fork latest",
			fork:    true,
			wantErr: false,
		},
		{
			name:    "--fork with valid uuid",
			fork:    true,
			forkID:  "11111111-2222-3333-4444-555555555555",
			wantErr: false,
		},
		{
			name:    "--resume with invalid uuid",
			resume:  "not-a-uuid",
			wantErr: true,
		},
		{
			name:    "--fork and --resume both set",
			fork:    true,
			resume:  "11111111-2222-3333-4444-555555555555",
			wantErr: true,
		},
		{
			name:    "--fork with invalid uuid",
			fork:    true,
			forkID:  "not-a-uuid",
			wantErr: true,
		},
		{
			name:    "positional argument present",
			args:    []string{"unexpected"},
			wantErr: true,
		},
		{
			name:    "--resume with malformed uuid",
			resume:  "11111111-2222-3333-4444-55555555555Z", // bad char at end
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
			err := validateFlags(tt.showVersion, tt.resume, tt.fork, tt.forkID, tt.args, tt.configPath, tt.noConfig)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateFlags(%v, %q, %v, %q, %v, %q, %v) = %v, wantErr=%v",
					tt.showVersion, tt.resume, tt.fork, tt.forkID, tt.args, tt.configPath, tt.noConfig, err, tt.wantErr)
			}
		})
	}
}

func TestValidateFlagsVersionShortCircuitSkipsArgCheck(t *testing.T) {
	// showVersion=true must not fail even with positional arguments.
	if err := validateFlags(true, "", false, "", []string{"oops"}, "", false); err != nil {
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

func TestNormalizeForkArgs(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want []string
	}{
		{name: "latest", args: []string{"-fork"}, want: []string{"-fork="}},
		{name: "uuid with separate value", args: []string{"--fork", "11111111-2222-3333-4444-555555555555"}, want: []string{"--fork=11111111-2222-3333-4444-555555555555"}},
		{name: "next flag means latest", args: []string{"--fork", "--no-session"}, want: []string{"--fork=", "--no-session"}},
		{name: "after terminator is prompt text", args: []string{"--", "--fork", "uuid"}, want: []string{"--", "--fork", "uuid"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeForkArgs(tt.args)
			if len(got) != len(tt.want) {
				t.Fatalf("normalizeForkArgs(%v) = %v, want %v", tt.args, got, tt.want)
			}
			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Errorf("normalizeForkArgs(%v)[%d] = %q, want %q", tt.args, i, got[i], tt.want[i])
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
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resumeCommand(tt.cfg, tt.configPath, tt.noConfig, id)
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
