package cake

import (
	"bytes"
	"encoding/json"
	"fmt"
)

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
	case "session_meta":
		var ev SessionMeta
		if err := json.Unmarshal(env.Raw, &ev); err != nil {
			return nil, fmt.Errorf("decoding %q event: %w", env.Type, err)
		}
		return ev, nil
	case "prompt_context":
		var ev PromptContext
		if err := json.Unmarshal(env.Raw, &ev); err != nil {
			return nil, fmt.Errorf("decoding %q event: %w", env.Type, err)
		}
		return ev, nil
	case "skill_activated":
		var ev SkillActivated
		if err := json.Unmarshal(env.Raw, &ev); err != nil {
			return nil, fmt.Errorf("decoding %q event: %w", env.Type, err)
		}
		return ev, nil
	case "replay_error":
		var ev ReplayError
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
	case "compaction":
		var ev Compaction
		if err := json.Unmarshal(env.Raw, &ev); err != nil {
			return nil, fmt.Errorf("decoding %q event: %w", env.Type, err)
		}
		return ev, nil
	case "compaction_start":
		var ev CompactionStart
		if err := json.Unmarshal(env.Raw, &ev); err != nil {
			return nil, fmt.Errorf("decoding %q event: %w", env.Type, err)
		}
		return ev, nil
	case "compaction_end":
		var ev CompactionEnd
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
	case "turn_usage":
		var ev TurnUsage
		if err := json.Unmarshal(env.Raw, &ev); err != nil {
			return nil, fmt.Errorf("decoding %q event: %w", env.Type, err)
		}
		return ev, nil
	default:
		return Unknown{Type: env.Type}, nil
	}
}
