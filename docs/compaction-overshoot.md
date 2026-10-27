# Measured between-turn compaction overshoot

This note records the boundary-only evidence for cake-repl's experimental
between-turn compactor. It is not a summary-quality claim.

Source: cake task 130 (Completed 2026-10-27), an offline analysis of 2,420
sessions. No provider calls were made. The longer workspace handoff is
`research/context-compaction-phase6-overshoot.md` in the coordinating workspace;
cake-repl does not ship that note.

## What was measured

- Growth per invocation (maximum measured input minus first measured input):
  median +40,375 tokens; p75 +89,360; p90 +156,599; p99 +311,592; max +556,317
  (n=2,153).
- Sixteen invocations crossed 400k from below. All 16 were still above 400k at
  their last measured request.
- Active time above the line: median 5.9 minutes and max 38.8 minutes at 350k;
  median 7.1 and max 29.8 at 400k.
- Anchor session `f8d30612`: invocation 2 opened at 59,360 tokens, reached
  134,560 in 4.1 minutes, then compacted from 134,903 to 65,126. Its next three
  invocations still started at 85,395, 88,401, and 97,849.

## What it means

`max-context-tokens` and `trigger-tokens` are between-prompt targets, not
in-invocation caps. The REPL sees a measurement only after a completed turn, so
one invocation can grow past both lines before the compactor gets a chance to
run. The next boundary then compacts from that larger measurement.

A request-boundary mechanism inside Cake is planned in core tasks 034f and 034g;
it is not implemented here. Until it lands, treat the compaction target as a
boundary policy with the overshoot measured above.
