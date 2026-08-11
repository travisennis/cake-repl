package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/travisennis/cake-repl/internal/app"
)

func TestValidateFlagsVersionShortCircuits(t *testing.T) {
	// --version should return nil even when other flags are invalid.
	if err := validateFlags(true, true, "not-a-uuid", []string{"surprise!"}, "/path", true); err != nil {
		t.Errorf("validateFlags(true, …) = %v, want nil", err)
	}
}

func TestValidateFlags(t *testing.T) {
	tests := []struct {
		name         string
		showVersion  bool
		continueFlag bool
		resume       string
		args         []string
		configPath   string
		noConfig     bool
		wantErr      bool
	}{
		{
			name:    "no flags, no args",
			wantErr: false,
		},
		{
			name:         "--continue alone",
			continueFlag: true,
			wantErr:      false,
		},
		{
			name:    "--resume with valid uuid",
			resume:  "11111111-2222-3333-4444-555555555555",
			wantErr: false,
		},
		{
			name:    "--resume with invalid uuid",
			resume:  "not-a-uuid",
			wantErr: true,
		},
		{
			name:         "--continue and --resume both set",
			continueFlag: true,
			resume:       "11111111-2222-3333-4444-555555555555",
			wantErr:      true,
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
			err := validateFlags(tt.showVersion, tt.continueFlag, tt.resume, tt.args, tt.configPath, tt.noConfig)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateFlags(%v, %v, %q, %v, %q, %v) = %v, wantErr=%v",
					tt.showVersion, tt.continueFlag, tt.resume, tt.args, tt.configPath, tt.noConfig, err, tt.wantErr)
			}
		})
	}
}

func TestValidateFlagsVersionShortCircuitSkipsArgCheck(t *testing.T) {
	// showVersion=true must not fail even with positional arguments.
	if err := validateFlags(true, false, "", []string{"oops"}, "", false); err != nil {
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
