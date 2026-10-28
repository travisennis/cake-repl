package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadMissingFile(t *testing.T) {
	cfg, err := Load("/tmp/nonexistent-cake-repl-config-abc123")
	if err != nil {
		t.Fatalf("Load on missing file = %v, want nil", err)
	}
	if cfg == nil || *cfg != (Config{}) {
		t.Fatalf("Load on missing file = %+v, want empty Config", cfg)
	}
}

func TestLoadValidFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte(`
model = "gpt-4"
profile = "fast"
output-limit = 5000
max-timeline-items = 200
cake-bin = "/usr/local/bin/cake"
tool-color = true
`), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load = %v", err)
	}
	if cfg.Model != "gpt-4" {
		t.Errorf("Model = %q, want %q", cfg.Model, "gpt-4")
	}
	if cfg.Profile != "fast" {
		t.Errorf("Profile = %q, want %q", cfg.Profile, "fast")
	}
	if cfg.OutputLimit != 5000 {
		t.Errorf("OutputLimit = %d, want %d", cfg.OutputLimit, 5000)
	}
	if cfg.MaxTimelineItems != 200 {
		t.Errorf("MaxTimelineItems = %d, want %d", cfg.MaxTimelineItems, 200)
	}
	if cfg.CakeBin != "/usr/local/bin/cake" {
		t.Errorf("CakeBin = %q, want %q", cfg.CakeBin, "/usr/local/bin/cake")
	}
	if !cfg.ToolColor {
		t.Error("ToolColor = false, want true")
	}
	if !cfg.ToolColorSet {
		t.Error("ToolColorSet = false, want true for a present tool-color key")
	}
}

func TestLoadInvalidSyntax(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.toml")
	if err := os.WriteFile(path, []byte(`model = `), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(path)
	if err == nil {
		t.Fatal("Load on bad TOML: want error, got nil")
	}
}

func TestLoadEmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.toml")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load on empty file = %v", err)
	}
	if cfg == nil || *cfg != (Config{}) {
		t.Fatalf("Load on empty file = %+v, want empty Config", cfg)
	}
}

func TestLoadDevNull(t *testing.T) {
	// --config /dev/null should succeed with an empty config
	cfg, err := Load("/dev/null")
	if err != nil {
		t.Fatalf("Load(/dev/null) = %v, want nil", err)
	}
	if cfg == nil || *cfg != (Config{}) {
		t.Fatalf("Load(/dev/null) = %+v, want empty Config", cfg)
	}
}

func TestMergeEmptySrc(t *testing.T) {
	dst := &Config{Model: "gpt-4", Profile: "fast"}
	got := Merge(dst, &Config{})
	if got.Model != "gpt-4" || got.Profile != "fast" {
		t.Errorf("Merge with empty src changed dst: %+v", got)
	}
}

func TestMergeOverrides(t *testing.T) {
	dst := &Config{Model: "old", Profile: "old", OutputLimit: 1000, MaxTimelineItems: 50, CakeBin: "old"}
	src := &Config{Model: "new", Profile: "new", OutputLimit: 5000, MaxTimelineItems: 200, CakeBin: "new", ToolColor: true, ToolColorSet: true}
	got := Merge(dst, src)
	if got.Model != "new" || got.Profile != "new" || got.OutputLimit != 5000 || got.MaxTimelineItems != 200 || got.CakeBin != "new" || !got.ToolColor {
		t.Errorf("Merge did not override all fields: %+v", got)
	}
}

func TestMergeNilSrc(t *testing.T) {
	dst := &Config{Model: "gpt-4"}
	got := Merge(dst, nil)
	if got.Model != "gpt-4" {
		t.Errorf("Merge with nil src changed dst: %+v", got)
	}
}

func TestMergePreservesDstZeroValues(t *testing.T) {
	dst := &Config{}
	src := &Config{Model: "m1", Profile: "p1"}
	got := Merge(dst, src)
	if got.Model != "m1" || got.Profile != "p1" {
		t.Errorf("Merge with zero dst did not apply src: %+v", got)
	}
}

func TestMergeSrcZeroDoesNotOverride(t *testing.T) {
	dst := &Config{Model: "existing", OutputLimit: 3000}
	src := &Config{Model: "", OutputLimit: 0}
	got := Merge(dst, src)
	if got.Model != "existing" || got.OutputLimit != 3000 {
		t.Errorf("Merge with zero src fields overwrote dst: %+v", got)
	}
}

// TestLoadMergeToolColorPrecedence decodes real files and merges them the way
// startup does: an explicit project-local `tool-color = false` must turn off an
// inherited XDG true, while an absent project key inherits the XDG value. The
// boolean's false value is meaningful, so it cannot ride the non-zero merge rule
// the other keys use.
func TestLoadMergeToolColorPrecedence(t *testing.T) {
	tests := []struct {
		name    string
		xdg     string
		project string
		want    bool
	}{
		{name: "project false overrides XDG true", xdg: "tool-color = true\n", project: "tool-color = false\n", want: false},
		{name: "absent project key inherits XDG true", xdg: "tool-color = true\n", project: "model = \"local\"\n", want: true},
		{name: "project true overrides XDG false", xdg: "tool-color = false\n", project: "tool-color = true\n", want: true},
		{name: "case-variant key is present", xdg: "Tool-Color = true\n", project: "", want: true},
		{name: "case-variant false overrides XDG true", xdg: "tool-color = true\n", project: "Tool-Color = false\n", want: false},
		{name: "absent everywhere stays false", xdg: "", project: "", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			xdgPath := filepath.Join(dir, "xdg.toml")
			projectPath := filepath.Join(dir, ".cake-repl.toml")
			if err := os.WriteFile(xdgPath, []byte(tt.xdg), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(projectPath, []byte(tt.project), 0o600); err != nil {
				t.Fatal(err)
			}

			xdgCfg, err := Load(xdgPath)
			if err != nil {
				t.Fatal(err)
			}
			projectCfg, _, err := LoadProject(projectPath)
			if err != nil {
				t.Fatal(err)
			}

			got := Merge(xdgCfg, projectCfg)
			if got.ToolColor != tt.want {
				t.Fatalf("effective ToolColor = %v, want %v", got.ToolColor, tt.want)
			}
		})
	}
}

func TestDefaultPaths(t *testing.T) {
	xdg, local := DefaultPaths()

	if !strings.Contains(xdg, "cake-repl") {
		t.Errorf("XDG path %q should contain cake-repl", xdg)
	}
	if xdg == "" {
		t.Error("XDG path should not be empty")
	}

	if local != ".cake-repl.toml" {
		t.Errorf("local path = %q, want %q", local, ".cake-repl.toml")
	}
}

func TestDefaultPathsRespectsXDGEnv(t *testing.T) {
	os.Setenv("XDG_CONFIG_HOME", "/custom/xdg") //nolint:errcheck // env ops don't fail in practice
	defer os.Unsetenv("XDG_CONFIG_HOME")        //nolint:errcheck // env ops don't fail in practice

	xdg, _ := DefaultPaths()
	want := "/custom/xdg/cake-repl/config.toml"
	if xdg != want {
		t.Errorf("XDG path = %q, want %q", xdg, want)
	}
}

func TestLoadProjectExcludesExecutable(t *testing.T) {
	for _, value := range []string{`"./repo-cake"`, `""`, `"\u001b[2Jrepo-cake"`} {
		t.Run(value, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), ".cake-repl.toml")
			content := "cake-bin = " + value + `
model = "local-model"
profile = "local-profile"
output-limit = 5000
max-timeline-items = 200
tool-color = true
`
			if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
			cfg, ignored, err := LoadProject(path)
			if err != nil || len(ignored) != 1 || ignored[0] != "cake-bin" {
				t.Fatalf("LoadProject ignored = %v, %v, want cake-bin ignored", ignored, err)
			}
			got := Merge(&Config{CakeBin: "../trusted-cake"}, cfg)
			want := Config{CakeBin: "../trusted-cake", Model: "local-model", Profile: "local-profile", OutputLimit: 5000, MaxTimelineItems: 200, ToolColor: true, ToolColorSet: true}
			if *got != want {
				t.Fatalf("merged config = %+v, want %+v", *got, want)
			}
		})
	}
}

func TestLoadProjectWithoutExecutable(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".cake-repl.toml")
	for _, content := range []string{"", `model = "local-model"`} {
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, ignored, err := LoadProject(path); err != nil || len(ignored) != 0 {
			t.Fatalf("LoadProject without executable-selecting keys = %v, %v", ignored, err)
		}
	}
	if _, ignored, err := LoadProject(filepath.Join(t.TempDir(), "missing")); err != nil || len(ignored) != 0 {
		t.Fatalf("LoadProject missing file = %v, %v", ignored, err)
	}
}

func TestLoadProjectInvalidSyntax(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".cake-repl.toml")
	if err := os.WriteFile(path, []byte(`cake-bin = `), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := LoadProject(path); err == nil {
		t.Fatal("LoadProject with invalid TOML: want error")
	}
}

func TestLoadCompactionTable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(`
[compaction]
enabled = true
cake-compact-bin = "/opt/bin/cake-compact"
threshold-percent = 70
keep-tokens = 12000
model = "summary-model"
profile = "summary-profile"
reasoning-effort = "low"
`), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load = %v", err)
	}
	want := Compaction{
		Enabled:          true,
		EnabledSet:       true,
		CakeCompactBin:   "/opt/bin/cake-compact",
		ThresholdPercent: 70,
		KeepTokens:       12000,
		Model:            "summary-model",
		Profile:          "summary-profile",
		ReasoningEffort:  "low",
	}
	if cfg.Compaction != want {
		t.Fatalf("Compaction = %+v, want %+v", cfg.Compaction, want)
	}
}

func TestLoadCompactionAbsoluteTable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(`
[compaction]
enabled = true
max-context-tokens = 400000
trigger-tokens = 350000
`), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load = %v", err)
	}
	want := Compaction{
		Enabled:          true,
		EnabledSet:       true,
		MaxContextTokens: 400000,
		TriggerTokens:    350000,
	}
	if cfg.Compaction != want {
		t.Fatalf("Compaction = %+v, want %+v", cfg.Compaction, want)
	}
}

func TestMergeCompactionDefaultsAndOverrides(t *testing.T) {
	base := DefaultCompaction()
	if base.Enabled || base.CakeCompactBin != DefaultCakeCompactBin ||
		base.MaxContextTokens != 0 || base.TriggerTokens != 0 ||
		base.ThresholdPercent != DefaultCompactionThresholdPercent ||
		base.KeepTokens != DefaultCompactionKeepTokens ||
		base.Model != "" || base.Profile != "" ||
		base.ReasoningEffort != "" {
		t.Fatalf("defaults = %+v", base)
	}

	file := &Config{Compaction: Compaction{
		Enabled:          true,
		EnabledSet:       true,
		ThresholdPercent: 60,
		Model:            "summary-model",
		Profile:          "summary-profile",
		ReasoningEffort:  "low",
	}}
	got := MergeCompaction(base, file)
	if !got.Enabled || got.ThresholdPercent != 60 ||
		got.Model != "summary-model" ||
		got.Profile != "summary-profile" ||
		got.ReasoningEffort != "low" {
		t.Fatalf("file values not applied: %+v", got)
	}
	if got.CakeCompactBin != DefaultCakeCompactBin || got.KeepTokens != DefaultCompactionKeepTokens ||
		got.MaxContextTokens != 0 || got.TriggerTokens != 0 {
		t.Fatalf("untouched defaults changed: %+v", got)
	}

	absolute := &Config{Compaction: Compaction{MaxContextTokens: 400000, TriggerTokens: 350000}}
	got = MergeCompaction(base, absolute)
	if got.MaxContextTokens != 400000 || got.TriggerTokens != 350000 {
		t.Fatalf("absolute values not applied: %+v", got)
	}
	if got := MergeCompaction(base, nil); got != base {
		t.Fatalf("nil file changed defaults: %+v", got)
	}
}

func TestMergeCompactionDisableWins(t *testing.T) {
	xdg := &Config{Compaction: Compaction{Enabled: true, EnabledSet: true, ThresholdPercent: 75}}
	local := &Config{Compaction: Compaction{Enabled: false, EnabledSet: true}}
	got := Merge(xdg, local)
	if got.Compaction.Enabled {
		t.Fatalf("project-local enabled=false did not disable: %+v", got.Compaction)
	}
	if got.Compaction.ThresholdPercent != 75 {
		t.Fatalf("unrelated threshold lost: %+v", got.Compaction)
	}
}

func TestLoadProjectCannotEnableOrSelectCompactor(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".cake-repl.toml")
	if err := os.WriteFile(path, []byte(`
[compaction]
enabled = true
cake-compact-bin = "/tmp/untrusted-compactor"
max-context-tokens = 500000
trigger-tokens = 1000
threshold-percent = 50
keep-tokens = 5000
model = "untrusted-model"
profile = "untrusted-profile"
reasoning-effort = "max"
`), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, ignored, err := LoadProject(path)
	if err != nil {
		t.Fatal(err)
	}
	wantIgnored := []string{
		"compaction.cake-compact-bin",
		"compaction.enabled",
		"compaction.max-context-tokens",
		"compaction.trigger-tokens",
		"compaction.threshold-percent",
		"compaction.keep-tokens",
		"compaction.model",
		"compaction.profile",
		"compaction.reasoning-effort",
	}
	if strings.Join(ignored, ",") != strings.Join(wantIgnored, ",") {
		t.Fatalf("ignored = %v, want %v", ignored, wantIgnored)
	}
	if cfg.Compaction.Enabled || cfg.Compaction.CakeCompactBin != "" ||
		cfg.Compaction.MaxContextTokens != 0 || cfg.Compaction.TriggerTokens != 0 ||
		cfg.Compaction.ThresholdPercent != 0 || cfg.Compaction.KeepTokens != 0 ||
		cfg.Compaction.Model != "" || cfg.Compaction.Profile != "" ||
		cfg.Compaction.ReasoningEffort != "" {
		t.Fatalf("project-local security or tuning keys survived: %+v", cfg.Compaction)
	}

	trusted := &Config{
		CakeBin: "cake",
		Compaction: Compaction{
			Enabled:          true,
			EnabledSet:       true,
			CakeCompactBin:   "trusted-compactor",
			MaxContextTokens: 400000,
			TriggerTokens:    350000,
			ThresholdPercent: 80,
			KeepTokens:       20000,
			Model:            "trusted-model",
			Profile:          "trusted-profile",
			ReasoningEffort:  "low",
		},
	}
	merged := Merge(trusted, cfg)
	if !merged.Compaction.Enabled || merged.Compaction.CakeCompactBin != "trusted-compactor" ||
		merged.Compaction.MaxContextTokens != 400000 || merged.Compaction.TriggerTokens != 350000 ||
		merged.Compaction.ThresholdPercent != 80 || merged.Compaction.KeepTokens != 20000 ||
		merged.Compaction.Model != "trusted-model" ||
		merged.Compaction.Profile != "trusted-profile" ||
		merged.Compaction.ReasoningEffort != "low" {
		t.Fatalf("trusted values did not survive: %+v", merged.Compaction)
	}
}

func TestLoadProjectCaseVariantKeysAreIgnored(t *testing.T) {
	// The TOML decoder matches struct fields case-insensitively, while
	// MetaData.IsDefined is case-sensitive. Presence checks must agree with the
	// decoder or a differently spelled key would survive provenance stripping.
	path := filepath.Join(t.TempDir(), ".cake-repl.toml")
	if err := os.WriteFile(path, []byte(`
Cake-Bin = "/tmp/untrusted-cake"

[Compaction]
Enabled = true
Cake-Compact-Bin = "/tmp/untrusted-compactor"
Max-Context-Tokens = 500000
Trigger-Tokens = 1000
Threshold-Percent = 1
Keep-Tokens = 1
Model = "untrusted-model"
Profile = "untrusted-profile"
Reasoning-Effort = "max"
`), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, ignored, err := LoadProject(path)
	if err != nil {
		t.Fatal(err)
	}
	wantIgnored := []string{
		"cake-bin",
		"compaction.cake-compact-bin",
		"compaction.enabled",
		"compaction.max-context-tokens",
		"compaction.trigger-tokens",
		"compaction.threshold-percent",
		"compaction.keep-tokens",
		"compaction.model",
		"compaction.profile",
		"compaction.reasoning-effort",
	}
	if strings.Join(ignored, ",") != strings.Join(wantIgnored, ",") {
		t.Fatalf("ignored = %v, want %v", ignored, wantIgnored)
	}
	if cfg.CakeBin != "" || cfg.Compaction.Enabled || cfg.Compaction.CakeCompactBin != "" ||
		cfg.Compaction.MaxContextTokens != 0 || cfg.Compaction.TriggerTokens != 0 ||
		cfg.Compaction.ThresholdPercent != 0 || cfg.Compaction.KeepTokens != 0 ||
		cfg.Compaction.Model != "" || cfg.Compaction.Profile != "" ||
		cfg.Compaction.ReasoningEffort != "" {
		t.Fatalf("case-variant project keys survived: cake-bin=%q compaction=%+v", cfg.CakeBin, cfg.Compaction)
	}
}

func TestLoadProjectCaseVariantDisableIsAllowed(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".cake-repl.toml")
	if err := os.WriteFile(path, []byte("[Compaction]\nEnabled = false\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, ignored, err := LoadProject(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(ignored) != 0 {
		t.Fatalf("ignored = %v, want none", ignored)
	}
	if !cfg.Compaction.EnabledSet || cfg.Compaction.Enabled {
		t.Fatalf("case-variant disable was not preserved: %+v", cfg.Compaction)
	}
}

func TestLoadProjectCanDisableCompaction(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".cake-repl.toml")
	if err := os.WriteFile(path, []byte("[compaction]\nenabled = false\nthreshold-percent = 60\nkeep-tokens = 5000\nmax-context-tokens = 400000\ntrigger-tokens = 350000\nmodel = \"untrusted-model\"\nprofile = \"untrusted-profile\"\nreasoning-effort = \"max\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, ignored, err := LoadProject(path)
	if err != nil {
		t.Fatal(err)
	}
	wantIgnored := []string{
		"compaction.max-context-tokens",
		"compaction.trigger-tokens",
		"compaction.threshold-percent",
		"compaction.keep-tokens",
		"compaction.model",
		"compaction.profile",
		"compaction.reasoning-effort",
	}
	if strings.Join(ignored, ",") != strings.Join(wantIgnored, ",") {
		t.Fatalf("ignored = %v, want %v", ignored, wantIgnored)
	}
	if !cfg.Compaction.EnabledSet || cfg.Compaction.Enabled {
		t.Fatalf("disable was not preserved: %+v", cfg.Compaction)
	}
	if cfg.Compaction.MaxContextTokens != 0 || cfg.Compaction.TriggerTokens != 0 ||
		cfg.Compaction.ThresholdPercent != 0 || cfg.Compaction.KeepTokens != 0 ||
		cfg.Compaction.Model != "" || cfg.Compaction.Profile != "" ||
		cfg.Compaction.ReasoningEffort != "" {
		t.Fatalf("project-local tuning survived: %+v", cfg.Compaction)
	}
	xdg := &Config{Compaction: Compaction{
		Enabled:          true,
		EnabledSet:       true,
		MaxContextTokens: 400000,
		TriggerTokens:    350000,
		ThresholdPercent: 70,
		KeepTokens:       12000,
	}}
	merged := Merge(xdg, cfg)
	if merged.Compaction.Enabled {
		t.Fatal("project-local disable did not override XDG enable")
	}
	if merged.Compaction.MaxContextTokens != 400000 || merged.Compaction.TriggerTokens != 350000 ||
		merged.Compaction.ThresholdPercent != 70 || merged.Compaction.KeepTokens != 12000 {
		t.Fatalf("trusted tuning did not survive: %+v", merged.Compaction)
	}
}

func TestCompactionValidate(t *testing.T) {
	tests := []struct {
		name    string
		c       Compaction
		wantErr bool
	}{
		{"disabled ignores values", Compaction{}, false},
		{"valid", Compaction{Enabled: true, CakeCompactBin: "cake-compact", ThresholdPercent: 80}, false},
		{"valid absolute", Compaction{Enabled: true, CakeCompactBin: "cake-compact", MaxContextTokens: 400000, TriggerTokens: 350000, ThresholdPercent: 80}, false},
		{"valid reasoning effort", Compaction{Enabled: true, CakeCompactBin: "cake-compact", ThresholdPercent: 80, ReasoningEffort: "low"}, false},
		{"invalid reasoning effort", Compaction{Enabled: true, CakeCompactBin: "cake-compact", ThresholdPercent: 80, ReasoningEffort: "maximal"}, true},
		{"absolute trigger adjacent to ceiling", Compaction{Enabled: true, CakeCompactBin: "cake-compact", MaxContextTokens: 400000, TriggerTokens: 399999, ThresholdPercent: 80}, false},
		{"missing binary", Compaction{Enabled: true, ThresholdPercent: 80}, true},
		{"threshold too low", Compaction{Enabled: true, CakeCompactBin: "x", ThresholdPercent: 0}, true},
		{"threshold too high", Compaction{Enabled: true, CakeCompactBin: "x", ThresholdPercent: 101}, true},
		{"negative keep", Compaction{Enabled: true, CakeCompactBin: "x", ThresholdPercent: 80, KeepTokens: -1}, true},
		{"absolute missing trigger", Compaction{Enabled: true, CakeCompactBin: "x", MaxContextTokens: 400000, ThresholdPercent: 80}, true},
		{"absolute missing ceiling", Compaction{Enabled: true, CakeCompactBin: "x", TriggerTokens: 350000, ThresholdPercent: 80}, true},
		{"absolute negative ceiling", Compaction{Enabled: true, CakeCompactBin: "x", MaxContextTokens: -1, TriggerTokens: 350000, ThresholdPercent: 80}, true},
		{"absolute negative trigger", Compaction{Enabled: true, CakeCompactBin: "x", MaxContextTokens: 400000, TriggerTokens: -1, ThresholdPercent: 80}, true},
		{"absolute trigger equals ceiling", Compaction{Enabled: true, CakeCompactBin: "x", MaxContextTokens: 400000, TriggerTokens: 400000, ThresholdPercent: 80}, true},
		{"absolute trigger above ceiling", Compaction{Enabled: true, CakeCompactBin: "x", MaxContextTokens: 400000, TriggerTokens: 400001, ThresholdPercent: 80}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.c.Validate(); (err != nil) != tt.wantErr {
				t.Fatalf("Validate() = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestLoadSuggestionsTable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte(`
[suggestions]
enabled = true
cake-suggest-bin = "/opt/bin/cake-suggest"
`), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	want := Suggestions{
		Enabled:        true,
		EnabledSet:     true,
		CakeSuggestBin: "/opt/bin/cake-suggest",
	}
	if cfg.Suggestions != want {
		t.Fatalf("Suggestions = %+v, want %+v", cfg.Suggestions, want)
	}
	if err := cfg.Suggestions.Validate(); err != nil {
		t.Fatalf("Validate = %v", err)
	}
}

func TestSuggestionsValidate(t *testing.T) {
	if err := (Suggestions{}).Validate(); err != nil {
		t.Fatalf("disabled empty suggestions = %v, want nil", err)
	}
	if err := (Suggestions{Enabled: true}).Validate(); err == nil {
		t.Fatal("enabled suggestions with no binary: want error")
	}
	if err := (Suggestions{Enabled: true, CakeSuggestBin: "cake-suggest"}).Validate(); err != nil {
		t.Fatalf("enabled suggestions with a binary = %v, want nil", err)
	}
}

func TestMergeSuggestionsDefaultsAndOverrides(t *testing.T) {
	base := DefaultSuggestions()
	if base.Enabled || base.CakeSuggestBin != DefaultCakeSuggestBin {
		t.Fatalf("DefaultSuggestions = %+v", base)
	}

	file := &Config{Suggestions: Suggestions{Enabled: true, EnabledSet: true}}
	got := MergeSuggestions(base, file)
	if !got.Enabled || got.CakeSuggestBin != DefaultCakeSuggestBin {
		t.Fatalf("MergeSuggestions = %+v", got)
	}

	file.Suggestions.CakeSuggestBin = "local-suggester"
	got = MergeSuggestions(base, file)
	if got.CakeSuggestBin != "local-suggester" {
		t.Fatalf("binary override lost: %+v", got)
	}

	if got := MergeSuggestions(base, nil); got != base {
		t.Fatalf("MergeSuggestions(nil) = %+v, want %+v", got, base)
	}
}

func TestMergeSuggestionsDisableWins(t *testing.T) {
	xdg := &Config{Suggestions: Suggestions{Enabled: true, EnabledSet: true, CakeSuggestBin: "xdg-suggester"}}
	local := &Config{Suggestions: Suggestions{Enabled: false, EnabledSet: true}}
	got := Merge(xdg, local)
	if got.Suggestions.Enabled {
		t.Fatalf("project-local enabled=false did not disable: %+v", got.Suggestions)
	}
	if got.Suggestions.CakeSuggestBin != "xdg-suggester" {
		t.Fatalf("unrelated executable lost: %+v", got.Suggestions)
	}
}

func TestLoadProjectCannotEnableOrSelectSuggester(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".cake-repl.toml")
	if err := os.WriteFile(path, []byte(`
[suggestions]
enabled = true
cake-suggest-bin = "/tmp/untrusted-suggester"
`), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, ignored, err := LoadProject(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Suggestions.Enabled || cfg.Suggestions.CakeSuggestBin != "" {
		t.Fatalf("project-local suggestion keys survived: %+v", cfg.Suggestions)
	}
	want := []string{"suggestions.cake-suggest-bin", "suggestions.enabled"}
	if strings.Join(ignored, ",") != strings.Join(want, ",") {
		t.Fatalf("ignored = %v, want %v", ignored, want)
	}

	// Disabling is allowed, and an untrusted file must not be able to turn an
	// already-enabled trusted setting back on in the same table.
	if writeErr := os.WriteFile(path, []byte("[suggestions]\nenabled = false\n"), 0o600); writeErr != nil {
		t.Fatal(writeErr)
	}
	cfg, ignored, err = LoadProject(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Suggestions.Enabled || !cfg.Suggestions.EnabledSet {
		t.Fatalf("project-local disable was not preserved: %+v", cfg.Suggestions)
	}
	if len(ignored) != 0 {
		t.Fatalf("ignored = %v, want none", ignored)
	}

	trusted := &Config{Suggestions: Suggestions{Enabled: true, EnabledSet: true, CakeSuggestBin: "trusted-suggester"}}
	merged := Merge(trusted, cfg)
	if merged.Suggestions.Enabled {
		t.Fatalf("trusted true survived a project-local false: %+v", merged.Suggestions)
	}
}
