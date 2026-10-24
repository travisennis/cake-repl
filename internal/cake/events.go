// Package cake provides a client for the cake CLI's stream-json output
// format. It treats cake as an external engine: events are decoded from
// newline-delimited JSON on stdout and exposed as typed Go values.
package cake

import (
	"bytes"
	"encoding/json"
	"strings"
	"unicode/utf8"
)

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

// TaskStart marks the beginning of one cake task. Model and ModelConfig are
// optional; cake#664 adds them to the live stream with the same meaning as on
// SessionMeta, and older cake binaries simply omit them.
type TaskStart struct {
	Type        string `json:"type"`
	SessionID   string `json:"session_id"`
	TaskID      string `json:"task_id"`
	Timestamp   string `json:"timestamp"`
	Model       string `json:"model,omitempty"`
	ModelConfig string `json:"model_config,omitempty"`
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
	Type      string           `json:"type"`
	ID        string           `json:"id"`
	Summary   ReasoningSummary `json:"summary,omitempty"`
	Timestamp string           `json:"timestamp,omitempty"`
}

// ReasoningSummary accepts both the historical string array and the typed
// summary objects emitted by newer providers. The UI intentionally does not
// render this provider-specific content, but decoding it must not turn a
// replay stream into a parse error.
type ReasoningSummary []string

func (s *ReasoningSummary) UnmarshalJSON(data []byte) error {
	var values []json.RawMessage
	if err := json.Unmarshal(data, &values); err != nil {
		return nil // Ignore an unfamiliar provider-specific summary shape.
	}
	out := make(ReasoningSummary, 0, len(values))
	for _, value := range values {
		var text string
		if json.Unmarshal(value, &text) == nil {
			out = append(out, text)
			continue
		}
		var object struct {
			Text string `json:"text"`
		}
		if json.Unmarshal(value, &object) == nil && object.Text != "" {
			out = append(out, object.Text)
		}
	}
	*s = out
	return nil
}

func (e Reasoning) EventType() string { return "reasoning" }

// SessionMeta describes session-level metadata in a replay stream. Cwd decodes
// cake's working_directory; Model is the provider model ID and ModelConfig is
// the [[models]] entry name the session was created with. Fields not needed by
// the REPL are deliberately ignored so cake can add metadata.
type SessionMeta struct {
	Type        string `json:"type"`
	SessionID   string `json:"session_id"`
	Cwd         string `json:"working_directory,omitempty"`
	Model       string `json:"model,omitempty"`
	ModelConfig string `json:"model_config,omitempty"`
	Profile     string `json:"profile,omitempty"`
	CreatedAt   string `json:"created_at,omitempty"`
	UpdatedAt   string `json:"updated_at,omitempty"`
}

func (e SessionMeta) EventType() string { return "session_meta" }

// PromptContext records context associated with one replayed prompt.
type PromptContext struct {
	Type      string `json:"type"`
	SessionID string `json:"session_id,omitempty"`
	TaskID    string `json:"task_id,omitempty"`
	Prompt    string `json:"prompt,omitempty"`
	Cwd       string `json:"cwd,omitempty"`
}

func (e PromptContext) EventType() string { return "prompt_context" }

// SkillActivated reports a skill that was active for a replayed task.
type SkillActivated struct {
	Type        string `json:"type"`
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
	Path        string `json:"path,omitempty"`
}

func (e SkillActivated) EventType() string { return "skill_activated" }

// ReplayError is the structured failure record emitted by cake replay.
type ReplayError struct {
	Type      string `json:"type"`
	SessionID string `json:"session_id"`
	Kind      string `json:"kind"`
	Error     string `json:"error"`
	ExitCode  int    `json:"exit_code"`
}

func (e ReplayError) EventType() string { return "replay_error" }

// Compaction is one committed context checkpoint, emitted by replay at its
// physical position in the transcript. It describes a summary plus a retained
// tail that replaced retired history; the REPL renders one boundary marker and
// seeds an estimated context size from it.
//
// Only what those two jobs need is decoded. The retained tail is reduced to its
// record count, and the summary is kept as a single bounded line, so a
// checkpoint never pins its retained messages, reasoning, or tool content in
// memory. The wire fields are type, checkpoint_version, checkpoint_id,
// session_id, task_id, timestamp, trigger, summary, retained,
// estimated_input, summary_instruction_version, and model.
type Compaction struct {
	CheckpointVersion         int
	CheckpointID              string
	SessionID                 string
	TaskID                    string
	Timestamp                 string
	Trigger                   string
	Summary                   string
	RetainedCount             int
	EstimatedInput            CompactionEstimate
	SummaryInstructionVersion int
	Model                     CompactionModel
}

// maxCompactionSummaryBytes bounds the summary line a decoded Compaction keeps.
// The record itself can be several megabytes; the event only needs enough text
// to stay recognizable in diagnostics, never the whole generated summary.
const maxCompactionSummaryBytes = 200

// CompactionEstimate is a checkpoint's own token estimate for the request
// before and after it. These are estimates, not provider measurements.
// Basis is cake's estimate basis (measured_usage or content_class).
type CompactionEstimate struct {
	BeforeTokens int
	AfterTokens  int
	Basis        string
}

// CompactionModel identifies the model a checkpoint was written under.
// Model is the provider model ID; ModelConfig is the [[models]] entry name.
type CompactionModel struct {
	Model       string
	ModelConfig string
}

func (e Compaction) EventType() string { return "compaction" }

// UnmarshalJSON decodes a compaction record field by field so an unknown or
// malformed optional field never drops the record from the stream: a field that
// does not decode keeps its zero value and the rest of the checkpoint still
// renders. A record that is not a JSON object is still an error, because the
// line itself is malformed.
func (e *Compaction) UnmarshalJSON(data []byte) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}

	decodeOptionalField(fields["checkpoint_version"], &e.CheckpointVersion)
	decodeOptionalField(fields["checkpoint_id"], &e.CheckpointID)
	decodeOptionalField(fields["session_id"], &e.SessionID)
	decodeOptionalField(fields["task_id"], &e.TaskID)
	decodeOptionalField(fields["timestamp"], &e.Timestamp)
	decodeOptionalField(fields["trigger"], &e.Trigger)
	decodeOptionalField(fields["summary_instruction_version"], &e.SummaryInstructionVersion)

	var summary string
	if decodeOptionalField(fields["summary"], &summary) {
		e.Summary = boundedFirstLine(summary, maxCompactionSummaryBytes)
	}

	var estimate map[string]json.RawMessage
	if decodeOptionalField(fields["estimated_input"], &estimate) {
		decodeOptionalField(estimate["before_tokens"], &e.EstimatedInput.BeforeTokens)
		decodeOptionalField(estimate["after_tokens"], &e.EstimatedInput.AfterTokens)
		decodeOptionalField(estimate["basis"], &e.EstimatedInput.Basis)
	}

	var model map[string]json.RawMessage
	if decodeOptionalField(fields["model"], &model) {
		decodeOptionalField(model["model"], &e.Model.Model)
		decodeOptionalField(model["model_config"], &e.Model.ModelConfig)
	}

	e.RetainedCount = countJSONArrayElements(fields["retained"])
	return nil
}

// decodeOptionalField decodes one optional field, reporting whether it decoded. A
// missing or malformed field is not an error: the event keeps decoding so one
// unexpected value cannot break the stream.
func decodeOptionalField(raw json.RawMessage, dst any) bool {
	if len(raw) == 0 {
		return false
	}
	return json.Unmarshal(raw, dst) == nil
}

// boundedFirstLine returns the first line of s, cut on a rune boundary to at
// most limit bytes.
func boundedFirstLine(s string, limit int) string {
	if line, _, ok := strings.Cut(s, "\n"); ok {
		s = line
	}
	if len(s) <= limit {
		return s
	}
	cut := limit
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}

// countJSONArrayElements counts the top-level elements of raw when it is a JSON
// array, and returns 0 otherwise. It walks the array with the tokenizer and
// discards each element instead of decoding it into a value, so the retained
// tail's message, reasoning, and tool payloads are never retained.
func countJSONArrayElements(raw json.RawMessage) int {
	if len(raw) == 0 {
		return 0
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	tok, err := dec.Token()
	if err != nil || tok != json.Delim('[') {
		return 0
	}
	count, depth := 0, 0
	for {
		tok, err = dec.Token()
		if err != nil {
			return count
		}
		switch v := tok.(type) {
		case json.Delim:
			switch v {
			case '[', '{':
				if depth == 0 {
					count++
				}
				depth++
			default: // ']' or '}'
				if depth == 0 {
					return count
				}
				depth--
			}
		default:
			if depth == 0 {
				count++
			}
		}
	}
}

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

// TaskComplete reports the final outcome of one cake task. Model and
// ModelConfig are optional and decode additively so a newer cake can report the
// resolved model identity on completion without breaking older consumers.
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
	Model         string `json:"model,omitempty"`
	ModelConfig   string `json:"model_config,omitempty"`
	Usage         Usage  `json:"usage"`
}

func (e TaskComplete) EventType() string { return "task_complete" }

// TurnUsage is the per-attempt token accounting cake reports for one
// main-agent provider attempt, in live and replayed streams. It is not part of
// the task aggregate: Usage describes that attempt only, so InputTokens is the
// prompt size the model saw for the turn and the basis for the context display.
// Attempt and TerminalClass are omitted for a first successful attempt.
// UsagePresence distinguishes a complete measurement from a partial one; older
// records omit it and carry complete usage. ContextWindow and
// RemainingContextTokens are optional: cake reports them only when the model
// configures a window and the usage is complete. Summary and compaction
// attempts are never streamed as context measurements.
type TurnUsage struct {
	Type                   string `json:"type"`
	SessionID              string `json:"session_id,omitempty"`
	TaskID                 string `json:"task_id,omitempty"`
	Turn                   int    `json:"turn"`
	Usage                  Usage  `json:"usage"`
	Attempt                int    `json:"attempt,omitempty"`
	TerminalClass          string `json:"terminal_class,omitempty"`
	UsagePresence          string `json:"usage_presence,omitempty"`
	Model                  string `json:"model,omitempty"`
	ResponseModel          string `json:"response_model,omitempty"`
	ProviderRequestID      string `json:"provider_request_id,omitempty"`
	ContextWindow          int    `json:"context_window,omitempty"`
	RemainingContextTokens int    `json:"remaining_context_tokens,omitempty"`
	Timestamp              string `json:"timestamp,omitempty"`
}

func (e TurnUsage) EventType() string { return "turn_usage" }

// Usage is cake's token accounting for one task or one provider attempt.
// The detail blocks are provider-specific and optional; unknown keys inside
// them are ignored like every other unknown field.
type Usage struct {
	InputTokens         int          `json:"input_tokens"`
	OutputTokens        int          `json:"output_tokens"`
	TotalTokens         int          `json:"total_tokens"`
	InputTokensDetails  UsageDetails `json:"input_tokens_details,omitempty"`
	OutputTokensDetails UsageDetails `json:"output_tokens_details,omitempty"`
}

// UsageDetails carries the optional breakdown cake attaches to a usage block.
// Which counters are populated depends on the provider, so a zero value means
// the provider did not report that counter.
type UsageDetails struct {
	CachedTokens     int `json:"cached_tokens,omitempty"`
	CacheWriteTokens int `json:"cache_write_tokens,omitempty"`
	ReasoningTokens  int `json:"reasoning_tokens,omitempty"`
}

// Unknown preserves a record whose type is not recognized. Only the type
// discriminator is retained; the full raw line is not copied (it is already
// written to the debug log by the runner). The stream keeps flowing so that
// forward-compatible decoding is cheap.
type Unknown struct {
	Type string
}

func (e Unknown) EventType() string { return e.Type }
