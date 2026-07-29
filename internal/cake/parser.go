package cake

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// ParseLine decodes one stream-json line into a typed event.
//
// Unknown event types are returned as Unknown rather than an error so the
// stream keeps flowing when cake adds new record types. Unknown fields on
// known types are ignored by encoding/json. A nil event with a nil error is
// returned for blank lines.
func ParseLine(line []byte) (Event, error) {
	trimmed := bytes.TrimSpace(line)
	if len(trimmed) == 0 {
		return nil, nil
	}

	var env StreamEnvelope
	if err := json.Unmarshal(trimmed, &env); err != nil {
		return nil, fmt.Errorf("malformed stream line: %w", err)
	}

	switch env.Type {
	case "task_start":
		var ev TaskStart
		if err := json.Unmarshal(env.Raw, &ev); err != nil {
			return nil, fmt.Errorf("decoding %q event: %w", env.Type, err)
		}
		return ev, nil
	case "message":
		var ev Message
		if err := json.Unmarshal(env.Raw, &ev); err != nil {
			return nil, fmt.Errorf("decoding %q event: %w", env.Type, err)
		}
		return ev, nil
	case "reasoning":
		var ev Reasoning
		if err := json.Unmarshal(env.Raw, &ev); err != nil {
			return nil, fmt.Errorf("decoding %q event: %w", env.Type, err)
		}
		return ev, nil
	case "function_call":
		var ev FunctionCall
		if err := json.Unmarshal(env.Raw, &ev); err != nil {
			return nil, fmt.Errorf("decoding %q event: %w", env.Type, err)
		}
		return ev, nil
	case "function_call_output":
		var ev FunctionCallOutput
		if err := json.Unmarshal(env.Raw, &ev); err != nil {
			return nil, fmt.Errorf("decoding %q event: %w", env.Type, err)
		}
		return ev, nil
	case "hook_event":
		var ev HookEvent
		if err := json.Unmarshal(env.Raw, &ev); err != nil {
			return nil, fmt.Errorf("decoding %q event: %w", env.Type, err)
		}
		return ev, nil
	case "task_complete":
		var ev TaskComplete
		if err := json.Unmarshal(env.Raw, &ev); err != nil {
			return nil, fmt.Errorf("decoding %q event: %w", env.Type, err)
		}
		return ev, nil
	default:
		return Unknown{Type: env.Type}, nil
	}
}
