---
status: accepted
date: 2026-10-26
decision-makers: Travis Ennis
---
# Show compaction progress in the REPL

## Context and Problem Statement

Between-turn compaction runs as a separate `cake-compact` child after a turn
finishes, and in a real session it took about 32 seconds. The composer, status
line, and terminal title already switched to `compacting`, but the timeline added
no row, so a user reading the conversation saw nothing change at the moment the
compactor started and could not tell whether it was progressing or stuck. The
status row carried no elapsed time and no cancellation hint.

A driven PTY session against a delayed fake compactor confirms the existing
indicators are rendered, not hidden by the renderer: the composer and status row
show `compacting` throughout. The gap is placement and persistence, not a
rendering defect.

## Decision Drivers

- A start must be conspicuous in the conversation the user is reading, not only
  in the one-row chrome.
- Progress must be visible while a long child runs, and Ctrl+C must be
  discoverable without typing.
- The start row must not outlive the operation: success, failure, cancellation,
  and no-op must all resolve it.
- No new timer, subprocess, or compactor protocol; the spinner tick already
  running while the child is alive can drive the elapsed display.

## Considered Options

- **Only strengthen the chrome.** Keep the status/composer state and add elapsed
  time there. Rejected as the only change: it still leaves no
  conversation-level start marker, which is what the report missed.
- **Add a timeline start row and leave it as history.** Rejected: the row keeps
  reading as in-progress after the child exits.
- **Add a timeline start row, add elapsed seconds and the cancellation hint to
  the persistent chrome, and resolve the row into the outcome (chosen).** The
  row becomes the success marker, the failure or cancellation warning, or an
  explicit skip notice once the outcome is known. A no-op is no longer silent,
  because the start row was shown.

## Decision Outcome

`startCompaction` appends one info row before launching the child:

`context compaction started · Ctrl+C cancels`

While the child runs, the status line and composer state read
`compacting <elapsed>` with whole seconds from the model's existing spinner
tick; no second timer or tick chain is introduced. The composer footer names
`Ctrl+C cancel · Ctrl+N new session`, so the hint stays visible even when the
timeline scrolls. When the result is reaped, the start row is removed and
exactly one outcome row is appended at the end of the timeline, using the
existing success/failure wording; a `compacted:false` document resolves it as
`context compaction skipped: <reason>`. Ctrl+N clears the timeline and discards
the old result; `/clear`, Ctrl+N, or the timeline cap can leave no start row to
resolve. The outcome is still appended for success, failure, or cancellation
when the row was merely cleared, while a no-op stays silent when its start row
is gone.

The session pin, the gate, cancellation, and the compactor protocol are
unchanged. This changes the no-op silence recorded in ADR 023; its session,
gating, and estimated-status rules stand.

### Consequences

- Good: a long compaction announces itself in the transcript, shows elapsed
  seconds, and ends in one explicit outcome row.
- Good: cancellation is discoverable from the composer without typing.
- Bad: one extra timeline row is written per compaction, and the elapsed text
  advances once per second while the child runs; both surfaces already redraw
  on every spinner frame, so this adds no new render cadence.
- Neutral: the start row is removed on resolution, so it is not a permanent
  audit line; the outcome row carries the durable information.

## More Information

- Task 123. The driven check uses a delayed fake `cake-compact` and the
  `turn-usage` fixture in a headless tmux pane.
- Partially supersedes
  [ADR 023](023-compact-context-between-turns-through-an-external-compactor.md)
  on no-op silence and the visible progress state only.
