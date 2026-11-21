---
status: accepted
date: 2026-11-20
---
# Gate operations by phase and request identity

## Context and Problem Statement

A run, replay, compaction, suggestion, and session listing each own a child
process. Their results arrive asynchronously and can outlive the action that
started them. Ad-hoc booleans made it possible for a canceled child to keep a
gate closed forever, or for a stale result to touch a newer request.

## Decision

`internal/app/lifecycle.go` defines one `pendingOperation` that owns the request
key, the context cancel function, and the reaping channel for a model-owned
child.

- A phase describes whether the child still holds a resource: starting, running,
  cancel requested, grace armed, group killed, or reaped. It does not describe
  what the result means.
- `operationKey` combines the session generation, a per-operation sequence, and
  the target session id. `settle` accepts only the active key. An invalidated
  key may close its drain, but it can never apply a result.
- A draining operation stays busy until its result is reaped. That keeps the
  session lock safe from a prompt or retarget racing the child.
- `cancel` keeps the key so the reaped result still settles the operation.
  `invalidate` moves the key out of the active slot immediately.
- Admission is capability-specific: `admitPrompt`, `admitRetarget`, and
  `admitSuggestion` share the resource phases but have distinct refusal wording.
  A suggestion deliberately ignores compaction and holds its result instead.
- Exit is a draining phase. Every quit path cancels every child and returns
  `tea.Quit` only after all of them are reaped. Waiters use done channels, not
  result messages, because the Update loop may already be gone.
- `completedTurn` is the single answer used by both post-turn operations. Only a
  clean completed turn for the current target qualifies.

## Consequences

- Good: stale results cannot settle a newer request.
- Good: input gates and retarget gates cannot disagree about whether a child is
  still active.
- Good: an unobserved exit still tears children down.
- Bad: every operation carries a key and a phase even when it is simple.

## More Information

- [`internal/app/lifecycle.go`](../../internal/app/lifecycle.go)
- [`internal/app/shutdown.go`](../../internal/app/shutdown.go)
- Related: [ADR 041](041-isolate-and-bound-every-external-child-process.md),
  [ADR 042](042-pin-the-session-target-by-announced-id.md)
