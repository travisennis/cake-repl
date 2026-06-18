package main

import (
	"os"
	"testing"
)

func TestSessionFilePathDefaultsToShare(t *testing.T) {
	os.Unsetenv("CAKE_DATA_DIR") //nolint:errcheck // env ops don't fail in practice
	got := sessionFilePath("home", "abc-123")
	want := "home/.local/share/cake/sessions/abc-123"
	if got != want {
		t.Errorf("sessionFilePath(\"home\", \"abc-123\") = %q, want %q", got, want)
	}
}

func TestSessionFilePathRespectsCAKEDATADIR(t *testing.T) {
	os.Setenv("CAKE_DATA_DIR", "/custom/data") //nolint:errcheck // env ops don't fail in practice
	defer os.Unsetenv("CAKE_DATA_DIR")         //nolint:errcheck // env ops don't fail in practice
	got := sessionFilePath("ignored", "abc-123")
	want := "/custom/data/sessions/abc-123"
	if got != want {
		t.Errorf("sessionFilePath(\"ignored\", \"abc-123\") = %q, want %q", got, want)
	}
}

func TestSessionFilePathEmptyCAKEDATADIRFallsBack(t *testing.T) {
	os.Setenv("CAKE_DATA_DIR", "")     //nolint:errcheck // env ops don't fail in practice
	defer os.Unsetenv("CAKE_DATA_DIR") //nolint:errcheck // env ops don't fail in practice
	got := sessionFilePath("home", "uuid-456")
	want := "home/.local/share/cake/sessions/uuid-456"
	if got != want {
		t.Errorf("sessionFilePath(\"home\", \"uuid-456\") = %q, want %q", got, want)
	}
}

func TestDisplayPathReplacesHomeWithTilde(t *testing.T) {
	got := displayPath("/Users/alice", "/Users/alice/.local/share/cake/sessions/abc")
	want := "~/.local/share/cake/sessions/abc"
	if got != want {
		t.Errorf("displayPath = %q, want %q", got, want)
	}
}

func TestDisplayPathLeavesNonHomePathUnchanged(t *testing.T) {
	got := displayPath("/Users/alice", "/custom/data/sessions/abc")
	want := "/custom/data/sessions/abc"
	if got != want {
		t.Errorf("displayPath = %q, want %q", got, want)
	}
}

func TestDisplayPathEmptyHomeReturnsPathUnchanged(t *testing.T) {
	got := displayPath("", "/some/path")
	want := "/some/path"
	if got != want {
		t.Errorf("displayPath = %q, want %q", got, want)
	}
}
