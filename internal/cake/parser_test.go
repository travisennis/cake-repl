package cake

import (
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"
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
			line: `{"type":"session_meta","session_id":"s-1","working_directory":"/tmp/project","new_field":true}`,
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

func TestParseLineSessionMetaModelIdentity(t *testing.T) {
	line := `{"type":"session_meta","format_version":4,"session_id":"s-1","timestamp":"2026-06-09T12:00:00Z","working_directory":"/tmp/project","model":"glm-5.1","model_config":"zen","tools":[],"future_metadata":true}`
	ev, err := ParseLine([]byte(line))
	if err != nil {
		t.Fatalf("ParseLine: %v", err)
	}
	meta, ok := ev.(SessionMeta)
	if !ok {
		t.Fatalf("got %T, want SessionMeta", ev)
	}
	if meta.SessionID != "s-1" || meta.Cwd != "/tmp/project" {
		t.Errorf("session fields = %+v, want session id and working_directory", meta)
	}
	if meta.Model != "glm-5.1" || meta.ModelConfig != "zen" {
		t.Errorf("model identity = %q/%q, want glm-5.1/zen", meta.Model, meta.ModelConfig)
	}
}

func TestParseLineSessionMetaWithoutModelIdentity(t *testing.T) {
	line := `{"type":"session_meta","session_id":"s-1","working_directory":"/tmp/project"}`
	ev, err := ParseLine([]byte(line))
	if err != nil {
		t.Fatalf("ParseLine: %v", err)
	}
	meta, ok := ev.(SessionMeta)
	if !ok {
		t.Fatalf("got %T, want SessionMeta", ev)
	}
	if meta.Model != "" || meta.ModelConfig != "" {
		t.Errorf("missing model identity decoded as %q/%q, want empty", meta.Model, meta.ModelConfig)
	}
}

func TestParseLineTaskStartModelIdentity(t *testing.T) {
	line := `{"type":"task_start","session_id":"s-1","task_id":"t-1","timestamp":"2026-06-09T12:00:00Z","model":"glm-5.1","model_config":"zen"}`
	ev, err := ParseLine([]byte(line))
	if err != nil {
		t.Fatalf("ParseLine: %v", err)
	}
	ts, ok := ev.(TaskStart)
	if !ok {
		t.Fatalf("got %T, want TaskStart", ev)
	}
	if ts.Model != "glm-5.1" || ts.ModelConfig != "zen" {
		t.Errorf("model identity = %q/%q, want glm-5.1/zen", ts.Model, ts.ModelConfig)
	}
}

func TestParseLineTaskStartWithoutModelIdentity(t *testing.T) {
	line := `{"type":"task_start","session_id":"s-1","task_id":"t-1","timestamp":"2026-06-09T12:00:00Z"}`
	ev, err := ParseLine([]byte(line))
	if err != nil {
		t.Fatalf("ParseLine: %v", err)
	}
	ts, ok := ev.(TaskStart)
	if !ok {
		t.Fatalf("got %T, want TaskStart", ev)
	}
	if ts.Model != "" || ts.ModelConfig != "" {
		t.Errorf("older task_start decoded model identity %q/%q, want empty", ts.Model, ts.ModelConfig)
	}
}

func TestParseLineTaskCompleteModelIdentity(t *testing.T) {
	line := `{"type":"task_complete","subtype":"success","is_error":false,"session_id":"s-1","task_id":"t-1","model":"glm-5.1","model_config":"zen","usage":{"input_tokens":1,"output_tokens":2,"total_tokens":3}}`
	ev, err := ParseLine([]byte(line))
	if err != nil {
		t.Fatalf("ParseLine: %v", err)
	}
	tc, ok := ev.(TaskComplete)
	if !ok {
		t.Fatalf("got %T, want TaskComplete", ev)
	}
	if tc.Model != "glm-5.1" || tc.ModelConfig != "zen" {
		t.Errorf("model identity = %q/%q, want glm-5.1/zen", tc.Model, tc.ModelConfig)
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

func TestParseLineTurnUsage(t *testing.T) {
	line := `{"type":"turn_usage","session_id":"s-1","task_id":"t-1","turn":3,"usage":{"input_tokens":14383,"input_tokens_details":{"cached_tokens":4096,"cache_write_tokens":0},"output_tokens":195,"output_tokens_details":{"reasoning_tokens":13},"total_tokens":14578},"attempt":2,"terminal_class":"retryable","timestamp":"2026-10-23T14:14:06Z","usage_presence":"complete","model":"deepseek-v4.1-flash","response_model":"deepseek-v4.1-flash","provider_request_id":"req-1","context_window":1000000,"remaining_context_tokens":985617}`
	ev, err := ParseLine([]byte(line))
	if err != nil {
		t.Fatalf("ParseLine: %v", err)
	}
	u, ok := ev.(TurnUsage)
	if !ok {
		t.Fatalf("got %T, want TurnUsage", ev)
	}
	if u.SessionID != "s-1" || u.TaskID != "t-1" || u.Turn != 3 {
		t.Errorf("identity/turn = %+v", u)
	}
	if u.Usage.InputTokens != 14383 || u.Usage.OutputTokens != 195 || u.Usage.TotalTokens != 14578 {
		t.Errorf("usage = %+v", u.Usage)
	}
	if u.Usage.InputTokensDetails.CachedTokens != 4096 || u.Usage.OutputTokensDetails.ReasoningTokens != 13 {
		t.Errorf("usage details = %+v/%+v", u.Usage.InputTokensDetails, u.Usage.OutputTokensDetails)
	}
	if u.Attempt != 2 || u.TerminalClass != "retryable" || u.UsagePresence != "complete" {
		t.Errorf("attempt metadata = %+v", u)
	}
	if u.Model != "deepseek-v4.1-flash" || u.ResponseModel != "deepseek-v4.1-flash" || u.ProviderRequestID != "req-1" {
		t.Errorf("model metadata = %+v", u)
	}
	if u.ContextWindow != 1000000 || u.RemainingContextTokens != 985617 {
		t.Errorf("context budget = %d/%d", u.ContextWindow, u.RemainingContextTokens)
	}
}

// A legacy or first-successful record omits the optional attempt metadata and
// the context budget. Decoding must leave those fields at their zero values,
// which the app treats as complete usage with an unknown window.
func TestParseLineTurnUsageLegacyRecord(t *testing.T) {
	line := `{"type":"turn_usage","session_id":"s-1","task_id":"t-1","turn":1,"usage":{"input_tokens":900,"output_tokens":10,"total_tokens":910}}`
	ev, err := ParseLine([]byte(line))
	if err != nil {
		t.Fatalf("ParseLine: %v", err)
	}
	u, ok := ev.(TurnUsage)
	if !ok {
		t.Fatalf("got %T, want TurnUsage", ev)
	}
	if u.Usage.InputTokens != 900 || u.Usage.TotalTokens != 910 {
		t.Errorf("usage = %+v", u.Usage)
	}
	if u.Attempt != 0 || u.TerminalClass != "" || u.UsagePresence != "" {
		t.Errorf("optional attempt metadata decoded as %+v", u)
	}
	if u.ContextWindow != 0 || u.RemainingContextTokens != 0 {
		t.Errorf("absent budget decoded as %d/%d", u.ContextWindow, u.RemainingContextTokens)
	}
}

// Unknown fields, including unknown detail counters, are ignored so a newer
// cake never turns a usage record into a parse error.
func TestParseLineTurnUsageIgnoresUnknownFields(t *testing.T) {
	line := `{"type":"turn_usage","turn":1,"usage":{"input_tokens":5,"output_tokens":1,"total_tokens":6,"input_tokens_details":{"cached_tokens":2,"future_counter":9},"future_usage_block":{"x":true}},"future_field":{"nested":true}}`
	ev, err := ParseLine([]byte(line))
	if err != nil {
		t.Fatalf("ParseLine: %v", err)
	}
	u, ok := ev.(TurnUsage)
	if !ok {
		t.Fatalf("got %T, want TurnUsage", ev)
	}
	if u.Usage.InputTokens != 5 || u.Usage.InputTokensDetails.CachedTokens != 2 {
		t.Errorf("known fields lost with unknown ones: %+v", u.Usage)
	}
}

// A compaction record decodes into the fields the timeline marker and the
// resumed status estimate need. Its retained tail is counted, not decoded, and
// its summary is reduced to the first line.
func TestParseLineCompaction(t *testing.T) {
	line := `{"type":"compaction","checkpoint_version":1,"checkpoint_id":"checkpoint-1","session_id":"s-1","task_id":"t-1","timestamp":"2026-10-23T14:00:08Z","trigger":"threshold","summary":"Earlier turns read README.md.\nRETAINED-SUMMARY-TAIL","retained":[{"type":"message","role":"user","content":"RETAINED-USER-INPUT"},{"type":"message","role":"assistant","content":"RETAINED-ANSWER"},{"type":"function_call","id":"fc-r","call_id":"call-9","name":"Read","arguments":"{}"},{"type":"function_call_output","call_id":"call-9","output":"RETAINED-TOOL-OUTPUT"}],"model":{"model":"gpt-5.4","model_config":"test"},"summary_instruction_version":1,"estimated_input":{"before_tokens":118000,"after_tokens":24000,"basis":"measured_usage"}}`
	ev, err := ParseLine([]byte(line))
	if err != nil {
		t.Fatalf("ParseLine: %v", err)
	}
	c, ok := ev.(Compaction)
	if !ok {
		t.Fatalf("got %T, want Compaction", ev)
	}
	if c.CheckpointVersion != 1 || c.CheckpointID != "checkpoint-1" {
		t.Errorf("checkpoint identity = %+v", c)
	}
	if c.SessionID != "s-1" || c.TaskID != "t-1" || c.Timestamp != "2026-10-23T14:00:08Z" {
		t.Errorf("record identity = %+v", c)
	}
	if c.Trigger != "threshold" || c.SummaryInstructionVersion != 1 {
		t.Errorf("record metadata = %+v", c)
	}
	if c.Summary != "Earlier turns read README.md." {
		t.Errorf("summary = %q, want the first line only", c.Summary)
	}
	if c.RetainedCount != 4 {
		t.Errorf("retained count = %d, want 4", c.RetainedCount)
	}
	want := CompactionEstimate{BeforeTokens: 118000, AfterTokens: 24000, Basis: "measured_usage"}
	if c.EstimatedInput != want {
		t.Errorf("estimated input = %+v, want %+v", c.EstimatedInput, want)
	}
	if c.Model.Model != "gpt-5.4" || c.Model.ModelConfig != "test" {
		t.Errorf("model = %+v", c.Model)
	}
}

// An unknown or malformed optional field must not drop the checkpoint: the
// marker still renders, and the fields that decoded stay usable.
func TestParseLineCompactionToleratesMalformedOptionalFields(t *testing.T) {
	line := `{"type":"compaction","checkpoint_version":"one","checkpoint_id":"checkpoint-1","trigger":"manual","summary":{"provider":"future"},"retained":{"not":"an array"},"estimated_input":"unknown","model":["future"],"summary_instruction_version":null,"future_field":{"nested":true}}`
	ev, err := ParseLine([]byte(line))
	if err != nil {
		t.Fatalf("ParseLine returned an error for malformed optional fields: %v", err)
	}
	c, ok := ev.(Compaction)
	if !ok {
		t.Fatalf("got %T, want Compaction", ev)
	}
	if c.CheckpointID != "checkpoint-1" || c.Trigger != "manual" {
		t.Errorf("well-formed fields lost: %+v", c)
	}
	if c.CheckpointVersion != 0 || c.Summary != "" || c.RetainedCount != 0 {
		t.Errorf("malformed fields decoded as %+v", c)
	}
	if c.EstimatedInput != (CompactionEstimate{}) || c.Model != (CompactionModel{}) {
		t.Errorf("malformed blocks decoded as %+v/%+v", c.EstimatedInput, c.Model)
	}
}

// The summary is bounded on a rune boundary, so one large generated summary
// cannot be pinned by the decoded event.
func TestParseLineCompactionBoundsSummary(t *testing.T) {
	summary := "a" + strings.Repeat("é", 2_000)
	line := `{"type":"compaction","summary":` + strconv.Quote(summary) + `}`
	ev, err := ParseLine([]byte(line))
	if err != nil {
		t.Fatalf("ParseLine: %v", err)
	}
	c, ok := ev.(Compaction)
	if !ok {
		t.Fatalf("got %T, want Compaction", ev)
	}
	if len(c.Summary) > maxCompactionSummaryBytes {
		t.Errorf("summary length = %d bytes, want at most %d", len(c.Summary), maxCompactionSummaryBytes)
	}
	if !utf8.ValidString(c.Summary) {
		t.Errorf("summary was cut mid-rune: %q", c.Summary)
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
