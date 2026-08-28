---
status: accepted
date: 2026-08-28
decision-makers: Travis Ennis
---
# Replay resumed sessions through cake stream-json

## Context and Problem Statement

`cake-repl -resume <uuid>` pins the next prompt to an existing cake session,
but the local TUI has no transcript until that prompt runs. cake 0.1.0 now
provides a read-only replay command that emits the session transcript as
stream-json records. Replay adds `session_meta`, `prompt_context`, and
`skill_activated` records to the live vocabulary and reports failures with a
structured `replay_error` record and a non-zero exit status.

The REPL must use this public CLI boundary. It must not read cake session files,
parse human output, or import cake code. A replay failure must not prevent the
user from continuing the explicitly selected session.

## Decision Drivers

- Show prior user and assistant conversation, tool activity, task boundaries,
and completion records before the first new prompt.
- Keep live prompt execution separate from read-only hydration and preserve the
existing `--resume <uuid>` session pin.
- Decode additive replay records and future fields without breaking the stream.
- Keep failures visible but non-fatal, including when an older cake binary does
not support replay.
- Do not expose raw stream content outside the existing debug-log and sanitized
rendering paths.

## Considered Options

- Read cake's JSONL session files directly: rejected because it crosses the
engine boundary and couples the REPL to cake's private storage.
- Parse cake's human-readable session output: rejected because it is not a
stable machine contract and loses structured event data.
- Invoke `cake --output-format stream-json replay <uuid>`: chosen because it is
the supported, read-only transcript contract and uses the existing NDJSON
runner and parser.

## Decision Outcome

Use a dedicated `cake.Replay` invocation with exactly
`--output-format stream-json replay <uuid>`. Start it from Bubble Tea's early
initialization path when the startup configuration contains `-resume`. Replay
runs in its own hydration state, so the status does not report a live prompt as
running and the first new prompt stays blocked until replay finishes.

Replay records pass through the same parser and event-to-timeline mapping as
live records. Hydration also renders replayed user messages; live prompt
submission continues to add its own user item. `session_meta` and task records
restore session context, while `prompt_context` and `skill_activated` are
structured, forward-compatible events whose unknown fields are ignored.

A structured `replay_error`, non-zero replay result, unsupported event, or
malformed line produces a timeline warning. The warning is non-fatal: after
replay ends, the next prompt still uses `--resume <uuid>`. Replay content is
subject to the existing ingest limits and render-boundary sanitization.

### Consequences

- Good: resumed sessions show their visible history before the next prompt.
- Good: cake remains an external engine and session-file access stays outside
cake-repl.
- Neutral: startup waits for replay before accepting a prompt, and large
transcripts remain subject to the configured timeline limits.
- Bad: older cake binaries without replay support show a warning and cannot
hydrate history, but the resumed session remains usable.

## More Information

- Task 032: hydrate resumed sessions from cake replay support.
- [`../../docs/guardrails/cake-integration-and-stream-json.md`](../guardrails/cake-integration-and-stream-json.md)
- [`../../ARCHITECTURE.md`](../../ARCHITECTURE.md)
- cake replay command: `cake --output-format stream-json replay <uuid>`
