---
status: accepted
date: 2026-11-20
---
# Bound retained tool output at ingest

## Context and Problem Statement

A single tool result can be tens of megabytes. The timeline keeps items for the
session so the user can expand output with `Ctrl+O`; retaining the raw result
without a bound can pin unreasonable memory. Truncating for display is a
different concern: display should be short by default, while expansion should
still show realistic output.

## Decision

- Tool output starts hidden. `Ctrl+O` cycles truncated, full, hidden, truncated
  for every tool block in the session and for blocks appended later.
- `DefaultOutputLimit` (2,000 bytes) is a rune-safe display budget. A cut never
  splits a rune.
- `maxRetainedToolOutputBytes` is 1 MiB and applies at ingest. It is deliberately
  far above the display limit: the display limit decides what is drawn, the
  retention bound decides what the session pins.
- `CapOutputBytes` measures the raw bytes, returns a fresh bounded copy, and
  appends the standard `… truncated (N bytes total)` marker with the original
  raw count. At or below the limit it returns the input unchanged.
- `TruncateOutput` trims trailing newlines before measuring and may return a
  substring of the original. It is for already-bounded display text, never for
  ingestion. The two contracts are easy to confuse; #8 tracks one clearer
  helper.
- Full mode renders retained output without the per-line padding lipgloss would
  add. A newline-heavy 1 MiB result otherwise caches a rendering multiplied by
  the terminal width. #9 tracks replacing that manual path.

## Consequences

- Good: a single oversized result cannot pin tens of megabytes for the session.
- Good: full mode remains useful for realistic output.
- Good: hidden-by-default keeps one header per tool call.
- Bad: two truncation helpers share a package; #8 tracks consolidation.
- Bad: full-mode wrapping is hand-rolled; #9 tracks a renderer path.

## More Information

- [`internal/ui/toolblock.go`](../../internal/ui/toolblock.go)
- [`internal/app/update.go`](../../internal/app/update.go)
- [`internal/ui/timeline.go`](../../internal/ui/timeline.go)
