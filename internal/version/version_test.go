package version

import "testing"

func TestBinaryDefault(t *testing.T) {
	if Binary != "dev" {
		t.Errorf("Binary = %q, want %q", Binary, "dev")
	}
}

func TestBinaryOverride(t *testing.T) {
	defer func(orig string) { Binary = orig }(Binary)
	Binary = "v1.0.0"
	if Binary != "v1.0.0" {
		t.Errorf("Binary = %q, want %q", Binary, "v1.0.0")
	}
}
