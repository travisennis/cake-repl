---
status: accepted
date: 2026-11-20
---
# Render native compaction as one lifecycle

## Context and Problem Statement

Cake compacts context on its own before a provider request. Its stream-json
records are `compaction_start` when a prepared candidate begins summary
generation, one `compaction` checkpoint when it commits, and `compaction_end` for
every terminal outcome including `no_op`, `no_fit`, and `failed`. Replay emits
the same physical records. Rendering only the checkpoint hides a skip or
failure, and a long operation shows no start row.

## Decision

- Native compaction state is separate from the opt-in external compactor. It is
  owned by the run, keyed by the pending start notice, and has no request key or
  cancellation of its own.
- A live `compaction_start` appends one start notice naming the trigger and the
  estimated before size. A start that arrives while an older lifecycle is
  pending resolves the older row first, so a malformed or truncated stream
  cannot leave two pending starts.
- A live checkpoint under a pending start contributes no second marker. Its
  estimates and retained count are kept as a fallback for the end.
- A committed end resolves the start into exactly one success marker and seeds
  the status estimate from the end's after figure. A committed end whose
  checkpoint was already rendered adds no second marker.
- A `no_op` or `no_fit` end resolves the start into one explicit skip row. A
  `failed` end resolves it into one warning. Skip and failure ends stay visible
  even with no start or checkpoint, because cake emits those attempts as an
  end-only record.
- A start with no end is resolved when the run finishes, so a row never keeps
  reading as pending.
- Replay ignores a start, renders a checkpoint as the one marker and estimate
  seed, adds nothing for a committed end, and shows a non-committed end.
- The start row is identified by a stable timeline id, never by row position or
  text. `/clear`, `/new`, Ctrl+N, and the timeline cap can remove it before the
  result arrives; the completion path handles its absence and still appends the
  outcome.

## Consequences

- Good: a live native compaction is visible from start to outcome.
- Good: a skip or failure is no longer silent.
- Good: a committed operation updates the estimated status immediately.
- Bad: two compaction wordings exist, one for cake's native operation and one
  for the external compactor.
- Neutral: an interrupted operation shows a canceled or incomplete row at run
  end because the stream may legally end without an end record.

## More Information

- [`internal/app/native_compaction.go`](../../internal/app/native_compaction.go)
- [`internal/app/update.go`](../../internal/app/update.go)
- [`internal/cake/events.go`](../../internal/cake/events.go)
- Related: [ADR 044](044-compact-context-between-turns-only-from-trusted-config.md),
  [ADR 053](053-decode-stream-json-forward-compatibly.md)
