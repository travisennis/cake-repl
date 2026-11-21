---
status: accepted
date: 2026-11-20
---
# Pin the session target by announced id and generation

## Context and Problem Statement

Cake stores sessions in the working directory. If the REPL starts a fresh cake
process for every prompt without a resume flag, another cake process in the same
directory can become the session the next prompt continues. That is session
hijack. The REPL also has to distinguish a session it has merely replayed from a
session the current invocation actually announced, because only the latter may
pin the next prompt.

## Decision

`internal/app/session.go` owns a pure `sessionState` state machine.

- A `RunResume` target always carries a non-empty `ResumeID`. A fresh target
  never keeps an unused id. The constructor and every transition enforce the
  invariant.
- `AnnouncedID` is set only when the current invocation reports a session id
  through `task_start`, or through completion when `task_start` did not. It is
  cleared whenever the target or turn selection changes.
- `OnTaskComplete` pins the next prompt to the announced id. `OnRunEnded` pins
  the id announced by any terminating run, including cancellation, a nonzero
  exit, a wait error, or a clean EOF before completion. Work cake already wrote
  stays resumable instead of being orphaned.
- `generation` advances only when the user leaves the target (`/new`, Ctrl+N,
  or a `/resume` pick that changes it), never when a run merely reports its own
  id. A stale result cannot settle or apply to a newer target.
- Replay hydration may set `SessionID` alone. A replayed id names the session
  but was not announced by the invocation being run, so it must not pin the
  next prompt.
- Retargeting clears the reported model, the context figure, and the announced
  id. The last completed result stays as history; it is not the current
  identity.
- `Fork` is used only for the initial fresh invocation. After cake reports the
  new session id, every later prompt uses `--resume`.

## Consequences

- Good: a parallel cake process cannot capture the conversation.
- Good: failed or canceled work remains resumable.
- Good: stale children cannot write into a new target.
- Bad: the state machine has several related fields and transitions; the
  invariant is carried by constructors and the ADR rather than comments.

## More Information

- [`internal/app/session.go`](../../internal/app/session.go)
- [`internal/app/lifecycle.go`](../../internal/app/lifecycle.go)
