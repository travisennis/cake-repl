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
	src := &Config{Model: "new", Profile: "new", OutputLimit: 5000, MaxTimelineItems: 200, CakeBin: "new", ToolColor: true}
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
			if err != nil || !ignored {
				t.Fatalf("LoadProject = %v, %v, want ignored key", ignored, err)
			}
			got := Merge(&Config{CakeBin: "../trusted-cake"}, cfg)
			want := Config{CakeBin: "../trusted-cake", Model: "local-model", Profile: "local-profile", OutputLimit: 5000, MaxTimelineItems: 200, ToolColor: true}
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
		if _, ignored, err := LoadProject(path); err != nil || ignored {
			t.Fatalf("LoadProject without cake-bin = %v, %v", ignored, err)
		}
	}
	if _, ignored, err := LoadProject(filepath.Join(t.TempDir(), "missing")); err != nil || ignored {
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
