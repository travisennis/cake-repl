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

	tests := []struct {
		name     string
		input    string
		sessions []string
		want     []string
		ok       bool
	}{
		{"no slash", "hello", nil, nil, false},
		{"empty string", "", nil, nil, false},
		{"just slash", "/", nil, []string{"/help", "/exit", "/new", "/continue", "/resume", "/session", "/clear", "/quit", "/q"}, true},
		{"partial help", "/h", nil, []string{"/help"}, true},
		{"partial help lower", "/he", nil, []string{"/help"}, true},
		{"partial exit", "/ex", nil, []string{"/exit"}, true},
		{"partial new", "/n", nil, []string{"/new"}, true},
		{"partial continue", "/co", nil, []string{"/continue"}, true},
		{"partial resume", "/re", nil, []string{"/resume"}, true},
		{"partial session", "/se", nil, []string{"/session"}, true},
		{"partial clear", "/cl", nil, []string{"/clear"}, true},
		{"q prefix", "/q", nil, []string{"/quit", "/q"}, true},
		{"quit prefix", "/qu", nil, []string{"/quit"}, true},
		{"unknown prefix", "/z", nil, nil, false},
		{"resume bare no sessions", "/resume ", nil, nil, false},
		{"resume bare with sessions", "/resume ", sessions, sessions, true},
		{"resume partial uuid", "/resume 1111", sessions, []string{"11111111-2222-3333-4444-555555555555"}, true},
		{"resume partial uuid mid", "/resume aaaa", sessions, []string{"aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"}, true},
		{"resume no match", "/resume xxxx", sessions, nil, false},
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
