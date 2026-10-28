---
status: accepted
date: 2026-10-28
decision-makers: Travis Ennis
---
# Run after-turn suggestions alongside compaction

## Context

ADR 028 starts a prompt suggestion only after any automatic compaction from the
same turn has exited and been reaped. The stated reason is to avoid reading a
session while the compactor appends to it. In practice a compaction summary can
take 30-60 seconds, and the suggestion is an independent, read-only model call,
so serializing them delays the suggestion by the compactor's full runtime.

Cake replay is a documented read-only projection that opens no lock and skips an
incomplete final record, which is the state an append-only writer can briefly
expose. A concurrent suggestion therefore reads either the pre-checkpoint
transcript or a valid checkpointed prefix, never a corrupt session. A
suggestion is also a hint about the conversation, not a projection of a
compaction checkpoint, so either view is acceptable.

## Decision

- One completed-turn transition starts the suggestion and the automatic
  compaction as concurrent Bubble Tea commands when both are eligible. The
  suggestion no longer waits for the compactor to start or finish.
- A suggestion result that arrives while compaction still gates the composer is
  held in model state instead of being applied. When the compactor settles, the
  held result is applied only if the request identity, session pin, empty
  composer, and idle-apply conditions still match.
- Typing, history recall, tab completion, `Ctrl+U`, a new session, a retarget,
  submit, and quit discard a held result exactly as they discard a visible
  ghost.
- `finishCompaction` no longer starts a suggestion. A completed turn makes at
  most one suggestion attempt, even when the suggestion result arrives before
  the compactor exits.
- This partially supersedes ADR 028's trigger clause ("If the turn starts an
  automatic compaction, the suggestion starts only after that compaction's
  result is reaped"). ADR 028 remains accepted for the invocation, result,
  apply, accept/dismiss, and failure contracts.

## Consequences

- Good: the suggestion model call overlaps the compactor's summary call, so the
  usable ghost is available as soon as compaction permits, rather than one
  suggestion latency after it.
- Good: the REPL keeps one Update loop and both children stay model-owned,
  bounded, and independently cancellable; no new session-file access is added.
- Neutral: a suggestion may be based on the pre-compaction transcript, while
  the compacted session carries a historical summary. The suggestion is a hint,
  not a persisted artifact.
- Bad: the end-of-turn path now has two in-flight children, so tests and
  cancellation reasoning must cover both identities and the held-result state.

## More Information

- [ADR 028](028-offer-after-turn-prompt-suggestions-from-an-external-suggester.md)
  defines the suggester contract partially superseded here.
- [ADR 023](023-compact-context-between-turns-through-an-external-compactor.md)
  defines the between-turn compactor and its session-append model.
- [ADR 026](026-show-compaction-progress-in-the-repl.md) defines the compaction
  gate and the composer state this hold preserves.
