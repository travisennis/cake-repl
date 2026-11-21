---
status: accepted
date: 2026-11-20
---
# Treat suggestions as a read-only background hint

## Context and Problem Statement

`cake-suggest` reads a finished session through cake's read-only replay stream
and returns one prompt suggestion. It is metered and runs after every completed
turn. It must never gate input, submit itself, cross a session boundary, or
overwrite a draft the user is typing.

## Decision

- The suggester is opt-in and trusted-config only. It is invoked only through
  `internal/suggest` with one JSON document result. It never reads or writes a
  session file.
- One request may run at a time. Its key includes the session generation, the
  request sequence, and the pinned session id. A result whose identity no longer
  matches is dropped.
- A request starts only when the composer is empty, the program is idle, and a
  completed turn is behind the pinned session. A clean startup replay that ends
  at a completed, non-errored turn is the other source of that evidence.
- The request starts beside an automatic compaction. The suggester reads through
  the read-only replay stream, which tolerates the compactor's concurrent
  append, so it does not wait. A usable result that arrives while the composer
  is gated is held and applied when the compactor is reaped.
- The status line reports `suggesting <n>s` beside the operational state while
  the request is in flight. It never changes the composer label, hint, or input
  gate.
- `Ctrl+S` on an empty composer copies the ghost into the composer as editable
  text. A second `Ctrl+S` submits it. The filled text bypasses slash-command
  parsing only while it remains exactly the text the suggestion wrote; any edit
  restores normal parsing.
- Typing and history recall hide the ghost but keep it available for restore
  when the composer is empty again. Completion, submit, retarget, and quit
  discard it. History recall also drops filled-text provenance, because the text
  left the composer.
- The suggestion text is normalized to one bounded, control-free line before it
  reaches the composer. Session content is model-generated and must not emit
  terminal controls through the placeholder.
- A broken suggester warns once per REPL process; the debug log still records
  every lifecycle result. A hung child is a failure after its fixed timeout, not
  a user cancellation.

## Consequences

- Good: suggestions never auto-submit and never overwrite a draft.
- Good: a suggestion can appear immediately after compaction without a second
  metered request.
- Good: a model-produced slash command cannot execute unless the user presses
  `Ctrl+S` twice without editing.
- Bad: the composer has a hidden ghost state and a filled-text provenance field.
- Neutral: suggestions are unavailable in a `-no-session` REPL because there is
  no pinned session to read.

## More Information

- [`internal/app/suggestion.go`](../../internal/app/suggestion.go)
- [`internal/suggest`](../../internal/suggest)
- Related: [ADR 044](044-compact-context-between-turns-only-from-trusted-config.md)
