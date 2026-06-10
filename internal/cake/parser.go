package cake

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ParseLine decodes one stream-json line into a typed event.
//
// Unknown event types are returned as Unknown rather than an error so the
// stream keeps flowing when cake adds new record types. Unknown fields on
// known types are ignored by encoding/json. A nil event with a nil error is
// returned for blank lines.
func ParseLine(line []byte) (Event, error) {
	trimmed := strings.TrimSpace(string(line))
	if trimmed == "" {
		return nil, nil
	}

	var env StreamEnvelope
	if err := json.Unmarshal([]byte(trimmed), &env); err != nil {
		return nil, fmt.Errorf("malformed stream line: %w", err)
	}

	decode := func(v Event) (Event, error) {
		if err := json.Unmarshal([]byte(trimmed), v); err != nil {
			return nil, fmt.Errorf("decoding %q event: %w", env.Type, err)
		}
		return v, nil
	}

	switch env.Type {
	case "task_start":
		ev, err := decode(&TaskStart{})
		if err != nil {
			return nil, err
		}
		return *ev.(*TaskStart), nil
	case "message":
		ev, err := decode(&Message{})
		if err != nil {
			return nil, err
		}
		return *ev.(*Message), nil
	case "reasoning":
		ev, err := decode(&Reasoning{})
		if err != nil {
			return nil, err
		}
		return *ev.(*Reasoning), nil
	case "function_call":
		ev, err := decode(&FunctionCall{})
		if err != nil {
			return nil, err
		}
		return *ev.(*FunctionCall), nil
	case "function_call_output":
		ev, err := decode(&FunctionCallOutput{})
		if err != nil {
			return nil, err
		}
		return *ev.(*FunctionCallOutput), nil
	case "hook_event":
		ev, err := decode(&HookEvent{})
		if err != nil {
			return nil, err
		}
		return *ev.(*HookEvent), nil
	case "task_complete":
		ev, err := decode(&TaskComplete{})
		if err != nil {
			return nil, err
		}
		return *ev.(*TaskComplete), nil
	default:
		return Unknown{Type: env.Type, Raw: trimmed}, nil
	}
}
