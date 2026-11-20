---
status: accepted
date: 2026-11-20
---
# Track Cake's native compaction lifecycle in the timeline

## Context and Problem Statement

Cake compacts context on its own before a provider request. Its stream-json
records describe that operation: `compaction_start` when a prepared candidate
begins summary generation, one `compaction` checkpoint when it commits, and
`compaction_end` for every terminal outcome, including `no_op`, `no_fit`, and
`failed`. Replay emits the same physical records.

The REPL decoded only the `compaction` checkpoint. During a live run it rendered
one marker and nothing else, so a skip or failure was invisible and a user could
not tell that Cake was compacting before the marker appeared. A committed
operation did not seed the status context until the next `turn_usage`, so the
status line stayed at the pre-compaction figure right after the transcript said
it had been compacted. Replay showed the checkpoint marker but no skip or
failure outcome.

## Decision Drivers

- Show the operation while it runs, and resolve it into exactly one outcome.
- Keep the native lifecycle separate from the opt-in between-turn
  `cake-compact` flow, which owns its own notice and gate.
- Never duplicate a checkpoint marker: start, checkpoint, and committed end
  describe one operation.
- A skip or failure must be visible, including an end-only record that has no
  checkpoint.
- Preserve the existing bare-checkpoint behavior for older Cake streams that
  emit no lifecycle records.

## Considered Options

- **Keep decoding only the checkpoint.** Rejected: skips and failures stay
  invisible, and a long native compaction shows no start row.
- **Add lifecycle handling to the external compactor state.** Rejected: the two
  operations differ in ownership, gating, cancellation, and wording. Sharing
  `compactionNotice` or the pending external operation would let one resolve
  the other's row.
- **Decode both lifecycle events, track native state keyed by the live start,
  and resolve it into one outcome (chosen).** The checkpoint stays a marker
  during replay and a fallback estimate during a live operation.

## Decision Outcome

`internal/cake` decodes `compaction_start` and `compaction_end` with the same
field-by-field tolerant decoding as `compaction`; a malformed optional field
keeps the record, and the end reason is bounded to the wire's 1,024 bytes.

`internal/app` keeps a separate native lifecycle on the model, keyed by the
pending start notice. Live behavior:

- A `compaction_start` appends one info notice naming the trigger and the
  estimated before size, for example
  `context compaction started · threshold, 118k before`.
- A `compaction` checkpoint while that notice is pending adds no second marker;
  its estimates and retained count are kept as a fallback for the end.
- A `committed` end resolves the notice into exactly one
  `context compacted …` marker and seeds the status estimate from the end's
  after estimate. `no_op` and `no_fit` resolve it into one
  `context compaction skipped: …` row; `failed` resolves it into one warning.
  An unknown future status becomes a generic info row instead of disappearing.
- A start with no end is resolved when the run finishes, so a start row never
  remains pending. A bare live checkpoint with no start or end keeps the
  existing marker and still does not seed the estimate.

Replay:

- A `compaction` checkpoint remains the one marker and the one estimate seed;
  a committed end adds nothing.
- A non-committed end is rendered because a skip may exist only as an end
  record.
- `compaction_start` is ignored so replay never leaves an unresolved notice.

`/clear`, `/new`, and `Ctrl+N` clear the native lifecycle with the rest of the
timeline state. The external `cake-compact` flow, its configuration, and its
`compacting` status state are unchanged.

### Consequences

- Good: a live native compaction is visible from start to outcome, and a skip
  or failure is no longer silent.
- Good: a committed operation updates the estimated status figure immediately
  instead of waiting for the next turn.
- Good: start, checkpoint, and end still produce exactly one marker per
  committed checkpoint in both live and replay streams.
- Bad: two compaction wordings now exist, one for Cake's native operation and
  one for the opt-in external compactor; both share the `context compacted …`
  success marker.
- Neutral: an interrupted live operation shows a canceled or incomplete row at
  run end because the stream may legitimately end without an end record.

## More Information

- [Cake compaction integration](../../../cake/cake/docs/integrations.md)
  defines the lifecycle records and their ordering. This ADR consumes that
  contract; it does not change it.
- [ADR 023](023-compact-context-between-turns-through-an-external-compactor.md)
  and [ADR 026](026-show-compaction-progress-in-the-repl.md) govern the external
  compactor. This decision does not supersede them.
