package app

import (
	"reflect"
	"testing"
)

func TestCompleteSlash(t *testing.T) {
	sessions := []string{
		"11111111-2222-3333-4444-555555555555",
		"aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
	}
	dupSessions := []string{
		"11111111-2222-3333-4444-555555555555",
		"11111111-2222-3333-4444-555555555555",
	}

	tests := []struct {
		name     string
		input    string
		sessions []string
		want     []string
		ok       bool
	}{
		{"no slash", "hello", nil, nil, false},
		{"empty string", "", nil, nil, false},
		{"just slash", "/", nil, []string{"/help", "/exit", "/new", "/resume", "/sessions", "/session", "/clear", "/quit", "/q"}, true},
		{"partial help", "/h", nil, []string{"/help"}, true},
		{"partial help lower", "/he", nil, []string{"/help"}, true},
		{"partial exit", "/ex", nil, []string{"/exit"}, true},
		{"partial new", "/n", nil, []string{"/new"}, true},
		{"partial resume", "/re", nil, []string{"/resume"}, true},
		{"partial session", "/se", nil, []string{"/sessions", "/session"}, true},
		{"partial clear", "/cl", nil, []string{"/clear"}, true},
		{"q prefix", "/q", nil, []string{"/quit", "/q"}, true},
		{"quit prefix", "/qu", nil, []string{"/quit"}, true},
		{"unknown prefix", "/z", nil, nil, false},
		{"resume bare no sessions", "/resume ", nil, nil, false},
		{"resume bare with sessions", "/resume ", sessions, []string{"/resume 11111111-2222-3333-4444-555555555555", "/resume aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"}, true},
		{"resume partial uuid", "/resume 1111", sessions, []string{"/resume 11111111-2222-3333-4444-555555555555"}, true},
		{"resume partial uuid mid", "/resume aaaa", sessions, []string{"/resume aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"}, true},
		{"resume no match", "/resume xxxx", sessions, nil, false},
		{"resume dedupes identical ids", "/resume ", dupSessions, []string{"/resume 11111111-2222-3333-4444-555555555555"}, true},
		{"partial just resume", "/re", sessions, []string{"/resume"}, true}, // not UUID — no space after
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := completeSlash(tt.input, tt.sessions)
			if ok != tt.ok {
				t.Errorf("completeSlash(%q) ok=%v, want %v", tt.input, ok, tt.ok)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("completeSlash(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

// TestCompletedResumeParsesAsCommand proves that a completion feeding the
// composer is a full command, so submitting it resumes a session instead of
// launching a model prompt carrying only a bare UUID.
func TestCompletedResumeParsesAsCommand(t *testing.T) {
	const session = "11111111-2222-3333-4444-555555555555"
	input := "/resume 1111"

	matches, ok := completeSlash(input, []string{session})
	if !ok {
		t.Fatalf("completeSlash(%q) ok=false, want true", input)
	}
	if len(matches) != 1 {
		t.Fatalf("completeSlash(%q) = %v, want 1 match", input, matches)
	}

	completed := matches[0]
	cmd, ok, err := ParseCommand(completed)
	if !ok {
		t.Fatalf("ParseCommand(%q) ok=false, want true", completed)
	}
	if err != nil {
		t.Fatalf("ParseCommand(%q) err=%v, want nil", completed, err)
	}
	if cmd.Kind != CmdResume {
		t.Errorf("ParseCommand(%q).Kind = %v, want CmdResume", completed, cmd.Kind)
	}
	if cmd.Arg != session {
		t.Errorf("ParseCommand(%q).Arg = %q, want %q", completed, cmd.Arg, session)
	}
}
