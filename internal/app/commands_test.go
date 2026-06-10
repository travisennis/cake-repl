package app

import "testing"

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
		{"/continue", CmdContinue},
		{"/session", CmdSession},
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

func TestParseCommandUnknown(t *testing.T) {
	_, ok, err := ParseCommand("/frobnicate")
	if !ok || err == nil {
		t.Errorf("unknown command should be ok=true with error, got ok=%v err=%v", ok, err)
	}
}
