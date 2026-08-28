package cake

import (
	"strings"
	"testing"
)

func TestParseLineTaskStart(t *testing.T) {
	line := `{"type":"task_start","session_id":"11111111-2222-3333-4444-555555555555","task_id":"t-1","timestamp":"2026-06-09T12:00:00Z"}`
	ev, err := ParseLine([]byte(line))
	if err != nil {
		t.Fatalf("ParseLine: %v", err)
	}
	ts, ok := ev.(TaskStart)
	if !ok {
		t.Fatalf("got %T, want TaskStart", ev)
	}
	if ts.SessionID != "11111111-2222-3333-4444-555555555555" || ts.TaskID != "t-1" {
		t.Errorf("unexpected fields: %+v", ts)
	}
}

func TestParseLineMessage(t *testing.T) {
	line := `{"type":"message","role":"assistant","content":"hello","status":"completed"}`
	ev, err := ParseLine([]byte(line))
	if err != nil {
		t.Fatalf("ParseLine: %v", err)
	}
	msg, ok := ev.(Message)
	if !ok {
		t.Fatalf("got %T, want Message", ev)
	}
	if msg.Role != "assistant" || msg.Content != "hello" {
		t.Errorf("unexpected fields: %+v", msg)
	}
}

func TestParseLineReasoning(t *testing.T) {
	line := `{"type":"reasoning","id":"r-1","summary":["thinking about it","more thoughts"]}`
	ev, err := ParseLine([]byte(line))
	if err != nil {
		t.Fatalf("ParseLine: %v", err)
	}
	r, ok := ev.(Reasoning)
	if !ok {
		t.Fatalf("got %T, want Reasoning", ev)
	}
	if len(r.Summary) != 2 || r.Summary[0] != "thinking about it" {
		t.Errorf("unexpected summary: %+v", r.Summary)
	}
}

func TestParseLineFunctionCallPair(t *testing.T) {
	callLine := `{"type":"function_call","id":"fc-1","call_id":"c-1","name":"bash","arguments":"{\"command\":\"ls\"}"}`
	ev, err := ParseLine([]byte(callLine))
	if err != nil {
		t.Fatalf("ParseLine call: %v", err)
	}
	fc, ok := ev.(FunctionCall)
	if !ok {
		t.Fatalf("got %T, want FunctionCall", ev)
	}
	if fc.CallID != "c-1" || fc.Name != "bash" || fc.Arguments != `{"command":"ls"}` {
		t.Errorf("unexpected fields: %+v", fc)
	}

	outLine := `{"type":"function_call_output","call_id":"c-1","output":"file.txt\n"}`
	ev, err = ParseLine([]byte(outLine))
	if err != nil {
		t.Fatalf("ParseLine output: %v", err)
	}
	out, ok := ev.(FunctionCallOutput)
	if !ok {
		t.Fatalf("got %T, want FunctionCallOutput", ev)
	}
	if out.CallID != "c-1" || out.Output != "file.txt\n" {
		t.Errorf("unexpected fields: %+v", out)
	}
}

func TestParseLineHookEvent(t *testing.T) {
	line := `{"type":"hook_event","event":"pre_tool_use","tool_name":"bash","decision":"deny","exit_code":2,"stderr":"nope","timestamp":"2026-06-09T12:00:00Z","task_id":"t-1","command":"./hook.sh","duration_ms":12,"fail_closed":false,"stdout":"","source_file":"hooks.toml"}`
	ev, err := ParseLine([]byte(line))
	if err != nil {
		t.Fatalf("ParseLine: %v", err)
	}
	h, ok := ev.(HookEvent)
	if !ok {
		t.Fatalf("got %T, want HookEvent", ev)
	}
	if h.Event != "pre_tool_use" || h.Decision != "deny" || h.ExitCode == nil || *h.ExitCode != 2 {
		t.Errorf("unexpected fields: %+v", h)
	}
}

func TestParseLineTaskComplete(t *testing.T) {
	line := `{"type":"task_complete","subtype":"success","is_error":false,"duration_ms":4200,"turn_count":3,"tool_call_count":5,"session_id":"s-1","task_id":"t-1","result":"all done","usage":{"input_tokens":100,"output_tokens":50,"total_tokens":150}}`
	ev, err := ParseLine([]byte(line))
	if err != nil {
		t.Fatalf("ParseLine: %v", err)
	}
	tc, ok := ev.(TaskComplete)
	if !ok {
		t.Fatalf("got %T, want TaskComplete", ev)
	}
	if tc.IsError || tc.DurationMS != 4200 || tc.Usage.TotalTokens != 150 {
		t.Errorf("unexpected fields: %+v", tc)
	}
}

func TestParseLineUnknownTypeIsSafe(t *testing.T) {
	line := `{"type":"shiny_new_record","whatever":42}`
	ev, err := ParseLine([]byte(line))
	if err != nil {
		t.Fatalf("ParseLine: %v", err)
	}
	u, ok := ev.(Unknown)
	if !ok {
		t.Fatalf("got %T, want Unknown", ev)
	}
	if u.Type != "shiny_new_record" {
		t.Errorf("unexpected type: %q", u.Type)
	}
}

func TestParseLineUnknownFieldsIgnored(t *testing.T) {
	line := `{"type":"message","role":"assistant","content":"hi","brand_new_field":{"nested":true}}`
	ev, err := ParseLine([]byte(line))
	if err != nil {
		t.Fatalf("ParseLine: %v", err)
	}
	if _, ok := ev.(Message); !ok {
		t.Fatalf("got %T, want Message", ev)
	}
}

func TestParseLineReplayRecords(t *testing.T) {
	tests := []struct {
		name string
		line string
		want Event
	}{
		{
			name: "session metadata",
			line: `{"type":"session_meta","session_id":"s-1","cwd":"/tmp/project","new_field":true}`,
			want: SessionMeta{SessionID: "s-1", Cwd: "/tmp/project"},
		},
		{
			name: "prompt context",
			line: `{"type":"prompt_context","session_id":"s-1","task_id":"t-1","prompt":"check this","extra":{"future":true}}`,
			want: PromptContext{SessionID: "s-1", TaskID: "t-1", Prompt: "check this"},
		},
		{
			name: "skill activation",
			line: `{"type":"skill_activated","name":"review","description":"Review code","unknown":"ignored"}`,
			want: SkillActivated{Name: "review", Description: "Review code"},
		},
		{
			name: "replay error",
			line: `{"type":"replay_error","session_id":"s-1","kind":"session_not_found","error":"missing","exit_code":3}`,
			want: ReplayError{SessionID: "s-1", Kind: "session_not_found", Error: "missing", ExitCode: 3},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseLine([]byte(tt.line))
			if err != nil {
				t.Fatalf("ParseLine: %v", err)
			}
			if got.EventType() != tt.want.EventType() {
				t.Fatalf("event type = %q, want %q", got.EventType(), tt.want.EventType())
			}
			switch want := tt.want.(type) {
			case SessionMeta:
				if got := got.(SessionMeta); got.SessionID != want.SessionID || got.Cwd != want.Cwd {
					t.Errorf("event = %+v, want %+v", got, want)
				}
			case PromptContext:
				if got := got.(PromptContext); got.SessionID != want.SessionID || got.TaskID != want.TaskID || got.Prompt != want.Prompt {
					t.Errorf("event = %+v, want %+v", got, want)
				}
			case SkillActivated:
				if got := got.(SkillActivated); got.Name != want.Name || got.Description != want.Description {
					t.Errorf("event = %+v, want %+v", got, want)
				}
			case ReplayError:
				if got := got.(ReplayError); got.Kind != want.Kind || got.Error != want.Error || got.ExitCode != want.ExitCode {
					t.Errorf("event = %+v, want %+v", got, want)
				}
			}
		})
	}
}

func TestParseLineReasoningTypedSummary(t *testing.T) {
	line := `{"type":"reasoning","id":"r-1","summary":[{"type":"summary_text","text":"typed summary"},"legacy summary"]}`
	ev, err := ParseLine([]byte(line))
	if err != nil {
		t.Fatalf("ParseLine: %v", err)
	}
	r, ok := ev.(Reasoning)
	if !ok {
		t.Fatalf("got %T, want Reasoning", ev)
	}
	if len(r.Summary) != 2 || r.Summary[0] != "typed summary" || r.Summary[1] != "legacy summary" {
		t.Errorf("summary = %#v", r.Summary)
	}
}

func TestParseLineUnknownReasoningSummaryShapeKeepsEvent(t *testing.T) {
	line := `{"type":"reasoning","id":"r-1","summary":{"provider":"future"}}`
	if ev, err := ParseLine([]byte(line)); err != nil {
		t.Fatalf("ParseLine returned error for unknown summary shape: %v", err)
	} else if _, ok := ev.(Reasoning); !ok {
		t.Fatalf("got %T, want Reasoning", ev)
	}
}

func TestParseLineMalformed(t *testing.T) {
	_, err := ParseLine([]byte(`{"type":"message", busted`))
	if err == nil {
		t.Fatal("want error for malformed JSON")
	}
	if !strings.Contains(err.Error(), "malformed") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestParseLineBlank(t *testing.T) {
	ev, err := ParseLine([]byte("   \n"))
	if err != nil || ev != nil {
		t.Fatalf("blank line should be skipped, got ev=%v err=%v", ev, err)
	}
}
