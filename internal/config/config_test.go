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
	src := &Config{Model: "new", Profile: "new", OutputLimit: 5000, MaxTimelineItems: 200, CakeBin: "new"}
	got := Merge(dst, src)
	if got.Model != "new" || got.Profile != "new" || got.OutputLimit != 5000 || got.MaxTimelineItems != 200 || got.CakeBin != "new" {
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
