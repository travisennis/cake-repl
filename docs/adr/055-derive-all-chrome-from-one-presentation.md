---
status: accepted
date: 2026-11-20
---
# Derive all chrome from one presentation

## Context and Problem Statement

The terminal title, status line, composer label, composer hint, and view body all
describe what the REPL is doing. When each surface derived that state
independently, one could name a run while another advertised a compaction
action.

## Decision

- `internal/app/presentationState` is the one derivation of the current
  activity. Its precedence follows the admission gates: exit, session browser,
  run, replay, compaction, then the idle composer.
- Idle is named `idle` on the status line and `ready` in the composer. The
  composer's label and the status line's label are separate fields so both
  existing surfaces keep their wording.
- A suggestion is presentation, not a request phase. It never changes the
  composer label, hint, or input gate; an in-flight request is a background
  indicator appended beside the operational state.
- The terminal title uses one format, `cake-repl: <cwd>`, with an operation
  marker while busy. The model emits it on every presentation transition because
  the status line and composer are not visible when the window is unfocused,
  behind another window, or reduced to a tab.
- `main` writes the idle title after `Program.Run` returns because the model
  cannot observe SIGINT, SIGTERM, or a panic.
- The status line sanitizes and flattens stream-derived fields to one row. The
  context segment shows the newest complete prompt measurement over the model
  window, prefixes an estimate with `~`, and omits the segment when no figure is
  available.
- The model identity prefers the reported `[[models]]` entry name, then the
  provider model ID, then the current turn selection. `/model` and `/profile`
  are idle-only, apply to the next prompt, and clear the reported identity and
  context figure until the next run reports its own.
- `/model` and `/profile` with `default` clear the selection.

## Consequences

- Good: all visible chrome agrees about the operation.
- Good: the title carries the same signal when the terminal is not focused.
- Bad: every new activity must be added to one precedence chain.
- Neutral: two labels exist for the idle state by design.

## More Information

- [`internal/app/presentation.go`](../../internal/app/presentation.go)
- [`internal/app/view.go`](../../internal/app/view.go)
- [`internal/ui/status.go`](../../internal/ui/status.go)
