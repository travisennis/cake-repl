---
status: accepted
date: 2026-11-20
---
# Decode stream-json forward-compatibly

## Context and Problem Statement

Cake is an external engine. Its stream-json schema can add event types and
fields without warning, and older cake binaries omit newer fields. The REPL must
keep rendering when either side is ahead, without importing cake internals or
reading its session files.

## Decision

- `internal/cake` decodes newline-delimited JSON from stdout into typed events.
  A blank line yields no event.
- An unknown event type becomes `Unknown` with its type discriminator, not an
  error. Unknown fields on known types are ignored by `encoding/json`.
- The compaction events decode field by field. A missing or malformed optional
  field keeps its zero value and the rest of the record still renders. A record
  that is not a JSON object is still an error because the line itself is
  malformed.
- A compaction record retains only a bounded first line of its summary and the
  count of retained records. The retained tail's messages, reasoning, and tool
  payloads are counted with the tokenizer and discarded, so a checkpoint never
  pins its contents.
- The native compaction reason is bounded to the first line and to the wire's
  1,024-byte cap. The summary is bounded to `maxCompactionSummaryBytes`.
- `TurnUsage` describes one provider attempt, not the task aggregate. A missing
  `usage_presence` is a legacy complete record; any other value is partial and
  is not a context measurement. A complete record replaces an estimated
  compaction figure.
- Session-level metadata is decoded additively. `session_meta` carries the model
  cake resolved for a replayed session; `TaskStart` and `TaskComplete` may also
  report model identity. The `[[models]]` entry name is preferred over the
  provider model ID because it stays resolvable when entries share an ID.
- Startup hydration uses cake's read-only `replay <uuid>` command through the
  same parser and subprocess lifecycle as a live run. It never supplies a prompt
  and never writes a session.
- The session browser uses only `cake sessions list --json`; the trust probe
  uses only `cake trust --status --json`. Neither reads a session file.
- A replayed start is audit metadata and is ignored; a replayed checkpoint is
  the one boundary marker. A replayed committed end adds nothing; a skip or
  failure end is shown because it may exist only as an end record.

## Consequences

- Good: a newer cake can add records without breaking the REPL.
- Good: an older cake can omit fields without breaking the REPL.
- Good: a multi-megabyte checkpoint does not pin its retained content.
- Bad: tolerant decoding can hide a malformed optional field; diagnostics are
  limited to the debug log and bounded reasons.

## More Information

- [`internal/cake/events.go`](../../internal/cake/events.go)
- [`internal/cake/parser.go`](../../internal/cake/parser.go)
- [`internal/cake/runner.go`](../../internal/cake/runner.go)
- Related: [ADR 054](054-render-native-compaction-as-one-lifecycle.md)
