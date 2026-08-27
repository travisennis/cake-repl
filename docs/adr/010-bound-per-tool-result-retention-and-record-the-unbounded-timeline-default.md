---
status: accepted
date: 2026-08-27
---
# Bound per-tool-result retention and record the unbounded timeline default

## Context and Problem Statement

A running REPL holds the whole transcript in memory: per-item structured data,
cached rendered strings, and the viewport's own copy. Two unbounded inputs
made session memory grow without a policy:

- One tool result of arbitrary size was retained in full for the life of the
  session. `applyEvent` stored `e.Output` verbatim; the render-time
  `-output-limit` only decided what was drawn, and `Ctrl+O` full mode existed
  precisely to show the rest, so nothing bounded what was pinned.
- `-max-timeline-items` defaults to no limit, so entry count was unbounded
  too.

The render caches also were not released when entries disappeared: `/clear`,
`Ctrl+N`, and `-max-timeline-items` trimming resliced slices, which keeps the
old backing arrays (and the trimmed strings) reachable.

## Decision Drivers

- A single oversized tool result must not pin tens of MB for the life of the
  session.
- `Ctrl+O` full mode must stay useful for realistic output, and must not
  change for normal-sized output.
- `/clear` and `Ctrl+N` must release the memory they appear to release.
- Keep the CLI and config surfaces unchanged.

## Decision Outcome

**Retained tool output is capped at 1 MiB per result at ingest.** Output beyond
the ceiling is cut at ingest with the same "… truncated (N bytes total)" marker
users already see in truncated mode; `Ctrl+O` full mode shows everything
retained, including that marker. The ceiling is a constant (1 MiB), deliberately
far above the default `-output-limit` of 2000 bytes and documented in
`README.md`; it is not a configurable flag.

**The `-max-timeline-items` default stays unbounded, deliberately.** Each
entry's memory is now individually bounded (retained output at most 1 MiB;
rendered forms bounded by `-output-limit`), so the unlimited default trades a
bounded per-entry cost for never silently dropping scroll-back history. Users
who want a bound on entry count set `-max-timeline-items`. This is a product
decision, recorded here so the unbounded default is not mistaken for an
oversight.

**Emptying or trimming the timeline actually releases memory.** `/clear` and
`Ctrl+N` drop the render cache's backing array; capped trimming copies the
survivors into fresh slices so trimmed payloads become unreachable instead of
staying alive behind a resliced header.

### Consequences

- Good: a 50 MB tool result now pins ~1.4 MB instead of ~50 MB (measured
  before/after in task 065).
- Good: `Ctrl+O` behavior is unchanged for output up to 1 MiB; full mode
  remains useful for realistic results.
- Neutral: full mode for oversized results ends at the ceiling, with a marker
  explaining the drop.
- Neutral: a capped timeline pays an O(MaxTimelineItems) copy per trimmed
  append — the cost of actually releasing the trimmed entries.

