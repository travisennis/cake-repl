---
status: accepted
date: 2026-08-01
decision-makers: Travis Ennis
---
# Bounded single-line reads from the cake stream

## Context and Problem Statement

The stdout reader (`internal/cake/runner.go`) reads records with
`bufio.Reader.ReadString('\n')` and no upper bound. Unbounded is deliberate —
`bufio.Scanner` was rejected so long tool outputs never abort the stream. The
cost: a malformed or runaway cake emitting one enormous or newline-less record
grows memory with no backstop until the OS kills the REPL, silently. cake is
trusted, so this is a robustness ceiling, not a vulnerability. A bound changes
the effective stream-json input contract, hence this ADR.

## Decision Drivers

- Long tool output must never abort the stream.
- Memory per record must have a ceiling.
- An oversized record should surface visibly and let the session continue,
  matching the resilience contract `ParseError` already provides.
- The ceiling must never fire in normal use: single-line tool outputs of
  several MB are plausible.

## Considered Options

1. **Stay unbounded.** No change; silent OOM on a runaway record is the
   failure mode.
2. **Bound and drain.** Ceiling per line; synthetic error event; skip to the
   next newline; stream continues.
3. **Bound and abort.** Kill the stream on oversize; reintroduces the Scanner
   problem.

## Decision Outcome

Chosen option: **2, bound and drain.**

Read with an explicit per-line ceiling of **16 MiB**, a tunable constant
(several-MB outputs are the plausible maximum; 16 MiB provides headroom).
When a line exceeds the ceiling: emit exactly one synthetic
`ParseError`-style timeline event naming the size, drain through the offending
newline, and continue parsing the next record with the stream alive. If
`-debug-log` is configured, the full oversized record is still written there —
the debug log is the designated raw-content sink, and the record is already in
memory, so this adds no memory and preserves the debug log's purpose. The
ceiling is validated against real workloads and tuned before release.

### Consequences

- Good, because per-record memory is bounded and the failure is visible rather
  than silent.
- Good, because the stream survives an oversized record; the following record
  parses normally.
- Neutral, because the debug log may hold one very large record; that is its
  contract.
- Bad, because records above the ceiling are rejected; the value must be
  re-validated if cake's realistic output sizes grow.

## More Information

- Task 070.
- `internal/cake/runner.go` read loop; `internal/cake/events.go` (`ParseError`).
- Task 065 (retained-memory policy) is related but separate.

