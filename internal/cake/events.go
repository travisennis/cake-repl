// Package cake provides a client for the cake CLI's stream-json output
// format. It treats cake as an external engine: events are decoded from
// newline-delimited JSON on stdout and exposed as typed Go values.
package cake

import "encoding/json"

// Event is implemented by every decoded stream-json record.
type Event interface {
	EventType() string
}

// StreamEnvelope captures the type discriminator and the raw JSON of a
// line in one pass, so the concrete event decode can reuse the raw bytes.
type StreamEnvelope struct {
	Type string
	Raw  json.RawMessage
}

func (e *StreamEnvelope) UnmarshalJSON(data []byte) error {
	e.Raw = data
	var tmp struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(data, &tmp); err != nil {
		return err
	}
	e.Type = tmp.Type
	return nil
}

// TaskStart marks the beginning of one cake task.
type TaskStart struct {
	Type      string `json:"type"`
	SessionID string `json:"session_id"`
	TaskID    string `json:"task_id"`
	Timestamp string `json:"timestamp"`
}

func (e TaskStart) EventType() string { return "task_start" }

// Message is one user/assistant message.
type Message struct {
	Type      string `json:"type"`
	Role      string `json:"role"`
	Content   string `json:"content"`
	ID        string `json:"id,omitempty"`
	Status    string `json:"status,omitempty"`
	Timestamp string `json:"timestamp,omitempty"`
}

func (e Message) EventType() string { return "message" }

// Reasoning carries model reasoning summaries when the provider emits them.
type Reasoning struct {
	Type      string   `json:"type"`
	ID        string   `json:"id"`
	Summary   []string `json:"summary,omitempty"`
	Timestamp string   `json:"timestamp,omitempty"`
}

func (e Reasoning) EventType() string { return "reasoning" }

// FunctionCall is a tool invocation requested by the model.
type FunctionCall struct {
	Type      string `json:"type"`
	ID        string `json:"id"`
	CallID    string `json:"call_id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
	Timestamp string `json:"timestamp,omitempty"`
}

func (e FunctionCall) EventType() string { return "function_call" }

// FunctionCallOutput is the result of an executed tool call.
type FunctionCallOutput struct {
	Type      string `json:"type"`
	CallID    string `json:"call_id"`
	Output    string `json:"output"`
	Timestamp string `json:"timestamp,omitempty"`
}

func (e FunctionCallOutput) EventType() string { return "function_call_output" }

// HookEvent reports one hook execution. Only the fields needed for display
// are decoded; everything else is ignored.
type HookEvent struct {
	Type             string `json:"type"`
	Event            string `json:"event"`
	ToolName         string `json:"tool_name,omitempty"`
	Command          string `json:"command,omitempty"`
	Decision         string `json:"decision,omitempty"`
	ResolvedDecision string `json:"resolved_decision,omitempty"`
	ExitCode         *int   `json:"exit_code,omitempty"`
	DurationMS       int64  `json:"duration_ms,omitempty"`
	Stderr           string `json:"stderr,omitempty"`
	Timestamp        string `json:"timestamp,omitempty"`
}

func (e HookEvent) EventType() string { return "hook_event" }

// TaskComplete reports the final outcome of one cake task.
type TaskComplete struct {
	Type          string `json:"type"`
	Subtype       string `json:"subtype"`
	IsError       bool   `json:"is_error"`
	DurationMS    int64  `json:"duration_ms"`
	TurnCount     int    `json:"turn_count"`
	ToolCallCount int    `json:"tool_call_count"`
	SessionID     string `json:"session_id"`
	TaskID        string `json:"task_id"`
	Result        string `json:"result,omitempty"`
	Error         string `json:"error,omitempty"`
	Usage         Usage  `json:"usage"`
}

func (e TaskComplete) EventType() string { return "task_complete" }

// Usage is the token accounting attached to a task completion.
type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	TotalTokens  int `json:"total_tokens"`
}

// Unknown preserves a record whose type is not recognized. Only the type
// discriminator is retained; the full raw line is not copied (it is already
// written to the debug log by the runner). The stream keeps flowing so that
// forward-compatible decoding is cheap.
type Unknown struct {
	Type string
}

func (e Unknown) EventType() string { return e.Type }
