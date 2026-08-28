package cake

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fixtureData reads a named NDJSON stream fixture from testdata.
func fixtureData(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "fixtures", name+".ndjson"))
	if err != nil {
		t.Fatalf("reading fixture: %v", err)
	}
	return data
}

// parseFixture parses every line of a stream fixture the way the runner does:
// malformed lines become synthetic ParseError events so the stream keeps
// flowing, and blank lines are skipped.
func parseFixture(t *testing.T, name string) []Event {
	t.Helper()
	var events []Event
	for _, line := range strings.Split(string(fixtureData(t, name)), "\n") {
		ev, parseErr := ParseLine([]byte(line))
		switch {
		case parseErr != nil:
			events = append(events, ParseError{Line: snippet(line, 200), Err: parseErr})
		case ev != nil:
			events = append(events, ev)
		}
	}
	return events
}

// wantTypes asserts the parsed stream has exactly the given event types in
// order, reporting the whole stream when it does not.
func wantTypes(t *testing.T, events []Event, types ...string) {
	t.Helper()
	got := make([]string, len(events))
	for i, ev := range events {
		got[i] = ev.EventType()
	}
	if strings.Join(got, ",") != strings.Join(types, ",") {
		t.Fatalf("event types = %v, want %v", got, types)
	}
}

func TestParseStreamHappyPath(t *testing.T) {
	events := parseFixture(t, "happy-path")
	wantTypes(t, events,
		"task_start", "message", "function_call", "function_call_output", "task_complete")

	start, ok := events[0].(TaskStart)
	if !ok {
		t.Fatalf("event 0 is %T, want TaskStart", events[0])
	}
	if start.SessionID != "11111111-2222-3333-4444-555555555555" || start.TaskID != "t-1" {
		t.Errorf("unexpected task_start fields: %+v", start)
	}

	msg, ok := events[1].(Message)
	if !ok {
		t.Fatalf("event 1 is %T, want Message", events[1])
	}
	if msg.Role != "assistant" || msg.Content != "hello there" {
		t.Errorf("unexpected message fields: %+v", msg)
	}

	call, ok := events[2].(FunctionCall)
	if !ok {
		t.Fatalf("event 2 is %T, want FunctionCall", events[2])
	}
	if call.CallID != "c-1" || call.Name != "bash" || call.Arguments != `{"command":"ls"}` {
		t.Errorf("unexpected function_call fields: %+v", call)
	}

	out, ok := events[3].(FunctionCallOutput)
	if !ok {
		t.Fatalf("event 3 is %T, want FunctionCallOutput", events[3])
	}
	if out.CallID != call.CallID || out.Output != "file.txt\n" {
		t.Errorf("unexpected function_call_output fields: %+v", out)
	}

	done, ok := events[4].(TaskComplete)
	if !ok {
		t.Fatalf("event 4 is %T, want TaskComplete", events[4])
	}
	if done.IsError || done.SessionID != start.SessionID || done.Usage.TotalTokens != 150 {
		t.Errorf("unexpected task_complete fields: %+v", done)
	}
}

func TestParseStreamErrorTask(t *testing.T) {
	events := parseFixture(t, "error-task")
	wantTypes(t, events, "task_start", "message", "task_complete")

	done, ok := events[2].(TaskComplete)
	if !ok {
		t.Fatalf("event 2 is %T, want TaskComplete", events[2])
	}
	if !done.IsError || done.Subtype != "error" || done.Error != "provider request failed" {
		t.Errorf("unexpected task_complete fields: %+v", done)
	}
}

func TestParseStreamMinimal(t *testing.T) {
	events := parseFixture(t, "minimal")
	wantTypes(t, events, "task_start", "task_complete")

	start, ok := events[0].(TaskStart)
	if !ok {
		t.Fatalf("event 0 is %T, want TaskStart", events[0])
	}
	done, ok := events[1].(TaskComplete)
	if !ok {
		t.Fatalf("event 1 is %T, want TaskComplete", events[1])
	}
	if done.SessionID != start.SessionID || done.TaskID != start.TaskID {
		t.Errorf("completion ids %q/%q do not match start %q/%q",
			done.SessionID, done.TaskID, start.SessionID, start.TaskID)
	}
	if done.IsError {
		t.Error("minimal stream should complete successfully")
	}
}

func TestParseStreamReplayPath(t *testing.T) {
	events := parseFixture(t, "replay-path")
	wantTypes(t, events,
		"session_meta", "task_start", "prompt_context", "message", "reasoning",
		"message", "function_call", "function_call_output", "task_complete")

	meta, ok := events[0].(SessionMeta)
	if !ok || meta.SessionID != "11111111-2222-3333-4444-555555555555" {
		t.Errorf("event 0 = %#v, want session metadata", events[0])
	}
	context, ok := events[2].(PromptContext)
	if !ok || context.TaskID != "old-task" || context.Prompt != "previous prompt" {
		t.Errorf("event 2 = %#v, want prompt context", events[2])
	}
	if user, ok := events[3].(Message); !ok || user.Role != "user" || user.Content != "previous prompt" {
		t.Errorf("event 3 = %#v, want replayed user message", events[3])
	}
	if _, ok := events[6].(FunctionCall); !ok {
		t.Errorf("event 6 = %T, want FunctionCall", events[6])
	}
}

func TestParseStreamUnknownTypeKeepsStreaming(t *testing.T) {
	events := parseFixture(t, "unknown-type")
	wantTypes(t, events, "task_start", "future_feature", "task_complete")

	unknown, ok := events[1].(Unknown)
	if !ok {
		t.Fatalf("event 1 is %T, want Unknown", events[1])
	}
	if unknown.Type != "future_feature" {
		t.Errorf("unexpected unknown event type: %q", unknown.Type)
	}
	if _, ok := events[2].(TaskComplete); !ok {
		t.Errorf("event 2 is %T, want TaskComplete after an unknown record", events[2])
	}
}

func TestParseStreamMalformedMidstreamKeepsStreaming(t *testing.T) {
	events := parseFixture(t, "malformed-midstream")
	wantTypes(t, events, "task_start", "parse_error", "task_complete")

	pe, ok := events[1].(ParseError)
	if !ok {
		t.Fatalf("event 1 is %T, want ParseError", events[1])
	}
	if !strings.Contains(pe.Line, "not json") || pe.Err == nil {
		t.Errorf("parse error did not preserve the raw line: %+v", pe)
	}
	if _, ok := events[2].(TaskComplete); !ok {
		t.Errorf("event 2 is %T, want TaskComplete after a malformed line", events[2])
	}
}
