package cake

import (
	"bytes"
	"encoding/json"
	"strings"
	"unicode/utf8"
)

type Event interface {
	EventType() string
}

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

type TaskStart struct {
	Type        string `json:"type"`
	SessionID   string `json:"session_id"`
	TaskID      string `json:"task_id"`
	Timestamp   string `json:"timestamp"`
	Model       string `json:"model,omitempty"`
	ModelConfig string `json:"model_config,omitempty"`
}

func (e TaskStart) EventType() string { return "task_start" }

type Message struct {
	Type      string `json:"type"`
	Role      string `json:"role"`
	Content   string `json:"content"`
	ID        string `json:"id,omitempty"`
	Status    string `json:"status,omitempty"`
	Timestamp string `json:"timestamp,omitempty"`
}

func (e Message) EventType() string { return "message" }

type Reasoning struct {
	Type      string           `json:"type"`
	ID        string           `json:"id"`
	Summary   ReasoningSummary `json:"summary,omitempty"`
	Timestamp string           `json:"timestamp,omitempty"`
}

type ReasoningSummary []string

func (s *ReasoningSummary) UnmarshalJSON(data []byte) error {
	var values []json.RawMessage
	if err := json.Unmarshal(data, &values); err != nil {
		return nil
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

type SessionMeta struct {
	Type        string            `json:"type"`
	SessionID   string            `json:"session_id"`
	Cwd         string            `json:"working_directory,omitempty"`
	Model       string            `json:"model,omitempty"`
	ModelConfig string            `json:"model_config,omitempty"`
	Profile     string            `json:"profile,omitempty"`
	RunConfig   *SessionRunConfig `json:"run_config,omitempty"`
	CreatedAt   string            `json:"created_at,omitempty"`
	UpdatedAt   string            `json:"updated_at,omitempty"`
}

type SessionRunConfig struct {
	Profile string `json:"profile,omitempty"`
}

func (e SessionMeta) EventType() string { return "session_meta" }

type PromptContext struct {
	Type      string `json:"type"`
	SessionID string `json:"session_id,omitempty"`
	TaskID    string `json:"task_id,omitempty"`
	Prompt    string `json:"prompt,omitempty"`
	Cwd       string `json:"cwd,omitempty"`
}

func (e PromptContext) EventType() string { return "prompt_context" }

type SkillActivated struct {
	Type        string `json:"type"`
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
	Path        string `json:"path,omitempty"`
}

func (e SkillActivated) EventType() string { return "skill_activated" }

type ReplayError struct {
	Type      string `json:"type"`
	SessionID string `json:"session_id"`
	Kind      string `json:"kind"`
	Error     string `json:"error"`
	ExitCode  int    `json:"exit_code"`
}

func (e ReplayError) EventType() string { return "replay_error" }

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

const maxCompactionSummaryBytes = 200

type CompactionEstimate struct {
	BeforeTokens int
	AfterTokens  int
	Basis        string
}

type CompactionModel struct {
	Model       string
	ModelConfig string
}

func (e Compaction) EventType() string { return "compaction" }

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

const (
	CompactionStatusCommitted = "committed"
	CompactionStatusNoOp      = "no_op"
	CompactionStatusNoFit     = "no_fit"
	CompactionStatusFailed    = "failed"
)

const maxCompactionReasonBytes = 1024

type CompactionStart struct {
	SessionID                 string
	TaskID                    string
	CheckpointID              string
	Timestamp                 string
	Trigger                   string
	EstimatedBeforeTokens     int
	ContextWindow             int
	SummaryInstructionVersion int
}

func (e CompactionStart) EventType() string { return "compaction_start" }

func (e *CompactionStart) UnmarshalJSON(data []byte) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}

	decodeOptionalField(fields["session_id"], &e.SessionID)
	decodeOptionalField(fields["task_id"], &e.TaskID)
	decodeOptionalField(fields["checkpoint_id"], &e.CheckpointID)
	decodeOptionalField(fields["timestamp"], &e.Timestamp)
	decodeOptionalField(fields["trigger"], &e.Trigger)
	decodeOptionalField(fields["estimated_before_tokens"], &e.EstimatedBeforeTokens)
	decodeOptionalField(fields["context_window"], &e.ContextWindow)
	decodeOptionalField(fields["summary_instruction_version"], &e.SummaryInstructionVersion)
	return nil
}

type CompactionEnd struct {
	SessionID             string
	TaskID                string
	CheckpointID          string
	Timestamp             string
	Trigger               string
	Status                string
	EstimatedBeforeTokens int
	EstimatedAfterTokens  int
	Basis                 string
	RetiredCount          int
	RetainedCount         int
	Reason                string
}

func (e CompactionEnd) EventType() string { return "compaction_end" }

func (e *CompactionEnd) UnmarshalJSON(data []byte) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}

	decodeOptionalField(fields["session_id"], &e.SessionID)
	decodeOptionalField(fields["task_id"], &e.TaskID)
	decodeOptionalField(fields["checkpoint_id"], &e.CheckpointID)
	decodeOptionalField(fields["timestamp"], &e.Timestamp)
	decodeOptionalField(fields["trigger"], &e.Trigger)
	decodeOptionalField(fields["status"], &e.Status)
	decodeOptionalField(fields["estimated_before_tokens"], &e.EstimatedBeforeTokens)
	decodeOptionalField(fields["estimated_after_tokens"], &e.EstimatedAfterTokens)
	decodeOptionalField(fields["basis"], &e.Basis)
	decodeOptionalField(fields["retired_count"], &e.RetiredCount)
	decodeOptionalField(fields["retained_count"], &e.RetainedCount)

	var reason string
	if decodeOptionalField(fields["reason"], &reason) {
		e.Reason = boundedFirstLine(reason, maxCompactionReasonBytes)
	}
	return nil
}

func decodeOptionalField(raw json.RawMessage, dst any) bool {
	if len(raw) == 0 {
		return false
	}
	return json.Unmarshal(raw, dst) == nil
}

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
			default:
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

type FunctionCall struct {
	Type      string `json:"type"`
	ID        string `json:"id"`
	CallID    string `json:"call_id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
	Timestamp string `json:"timestamp,omitempty"`
}

func (e FunctionCall) EventType() string { return "function_call" }

type FunctionCallOutput struct {
	Type      string `json:"type"`
	CallID    string `json:"call_id"`
	Output    string `json:"output"`
	Timestamp string `json:"timestamp,omitempty"`
}

func (e FunctionCallOutput) EventType() string { return "function_call_output" }

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

type Usage struct {
	InputTokens         int          `json:"input_tokens"`
	OutputTokens        int          `json:"output_tokens"`
	TotalTokens         int          `json:"total_tokens"`
	InputTokensDetails  UsageDetails `json:"input_tokens_details,omitempty"`
	OutputTokensDetails UsageDetails `json:"output_tokens_details,omitempty"`
}

type UsageDetails struct {
	CachedTokens     int `json:"cached_tokens,omitempty"`
	CacheWriteTokens int `json:"cache_write_tokens,omitempty"`
	ReasoningTokens  int `json:"reasoning_tokens,omitempty"`
}

type Unknown struct {
	Type string
}

func (e Unknown) EventType() string { return e.Type }
