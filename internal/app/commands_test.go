package app

import (
	"strings"
	"testing"
)

func TestHelpTextMentionsHydrationControls(t *testing.T) {
	for _, want := range []string{
		"exit (cancels a running task, hydration, or compaction first)",
		"ctrl+c           cancel running task or compaction; cancel hydration and exit; quit when idle",
		"ctrl+n           start a new session (cancels a running task, hydration, or compaction)",
	} {
		if !strings.Contains(HelpText, want) {
			t.Errorf("HelpText missing %q", want)
		}
	}
}

func TestParseCommandNotACommand(t *testing.T) {
	for _, in := range []string{"hello", "what is /help?", "  plain prompt  "} {
		if _, ok, _ := ParseCommand(in); ok {
			t.Errorf("%q should not parse as a command", in)
		}
	}
}

func TestParseCommandKnown(t *testing.T) {
	tests := []struct {
		in   string
		want CommandKind
	}{
		{"/help", CmdHelp},
		{"/exit", CmdExit},
		{"/quit", CmdExit},
		{"/q", CmdExit},
		{"/new", CmdNew},
		{"/session", CmdSession},
		{"/model", CmdModel},
		{"/profile", CmdProfile},
		{"/clear", CmdClear},
		{"  /HELP  ", CmdHelp},
	}
	for _, tt := range tests {
		cmd, ok, err := ParseCommand(tt.in)
		if !ok || err != nil {
			t.Errorf("ParseCommand(%q) ok=%v err=%v", tt.in, ok, err)
			continue
		}
		if cmd.Kind != tt.want {
			t.Errorf("ParseCommand(%q) kind=%v, want %v", tt.in, cmd.Kind, tt.want)
		}
	}
}

func TestParseCommandResume(t *testing.T) {
	cmd, ok, err := ParseCommand("/resume 11111111-2222-3333-4444-555555555555")
	if !ok || err != nil {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if cmd.Kind != CmdResume || cmd.Arg != "11111111-2222-3333-4444-555555555555" {
		t.Errorf("unexpected: %+v", cmd)
	}
}

func TestParseCommandResumeInvalidUUID(t *testing.T) {
	for _, in := range []string{"/resume", "/resume not-a-uuid", "/resume abc 123"} {
		_, ok, err := ParseCommand(in)
		if !ok {
			t.Errorf("%q should be recognized as a command attempt", in)
		}
		if err == nil {
			t.Errorf("%q should return an error", in)
		}
	}
}

func TestParseCommandSessionCopy(t *testing.T) {
	cmd, ok, err := ParseCommand("/session copy")
	if !ok || err != nil {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if cmd.Kind != CmdSession || cmd.Arg != "copy" {
		t.Errorf("unexpected: %+v", cmd)
	}

	if _, ok, err := ParseCommand("/SESSION COPY"); !ok || err != nil {
		t.Errorf("case-insensitive copy should parse, ok=%v err=%v", ok, err)
	}
}

func TestParseCommandSessionRejectsOtherArgs(t *testing.T) {
	for _, in := range []string{"/session show", "/session copy extra"} {
		_, ok, err := ParseCommand(in)
		if !ok {
			t.Errorf("%q should be recognized as a command attempt", in)
		}
		if err == nil {
			t.Errorf("%q should return an error", in)
		}
	}
}

func TestParseCommandModelAndProfile(t *testing.T) {
	tests := []struct {
		in   string
		kind CommandKind
		arg  string
	}{
		{"/model", CmdModel, ""},
		{"/MODEL", CmdModel, ""},
		{"/model gpt-x", CmdModel, "gpt-x"},
		{"/model default", CmdModel, "default"},
		{"/model DEFAULT", CmdModel, "default"},
		{`/model "gpt x"`, CmdModel, "gpt x"},
		{"/profile", CmdProfile, ""},
		{"/profile work", CmdProfile, "work"},
		{"/profile default", CmdProfile, "default"},
		{"/profile 'work profile'", CmdProfile, "work profile"},
	}
	for _, tt := range tests {
		cmd, ok, err := ParseCommand(tt.in)
		if !ok || err != nil {
			t.Errorf("ParseCommand(%q) ok=%v err=%v", tt.in, ok, err)
			continue
		}
		if cmd.Kind != tt.kind || cmd.Arg != tt.arg {
			t.Errorf("ParseCommand(%q) = %+v, want kind=%v arg=%q", tt.in, cmd, tt.kind, tt.arg)
		}
	}
}

func TestParseCommandModelAndProfileRejectInvalidArguments(t *testing.T) {
	for _, in := range []string{
		`/model ""`,
		`/model ''`,
		`/model " "`,
		`/model "gpt-x" extra`,
		`/model gpt-x extra`,
		`/model "gpt-x`,
		`/profile ""`,
		`/profile work extra`,
	} {
		_, ok, err := ParseCommand(in)
		if !ok {
			t.Errorf("%q should be recognized as a command attempt", in)
		}
		if err == nil {
			t.Errorf("%q should return an error", in)
		}
	}
}

func TestHelpTextMentionsModelAndProfileCommands(t *testing.T) {
	for _, want := range []string{
		"/model [name]",
		"/profile [name]",
		"/new, /resume, /sessions, /model, and /profile require an idle REPL",
	} {
		if !strings.Contains(HelpText, want) {
			t.Errorf("HelpText missing %q", want)
		}
	}
}

func TestParseCommandUnknown(t *testing.T) {
	_, ok, err := ParseCommand("/frobnicate")
	if !ok || err == nil {
		t.Errorf("unknown command should be ok=true with error, got ok=%v err=%v", ok, err)
	}
}

func TestParseCommandContinueRemoved(t *testing.T) {
	_, ok, err := ParseCommand("/continue")
	if !ok || err == nil {
		t.Errorf("removed /continue should be rejected, got ok=%v err=%v", ok, err)
	}
}
