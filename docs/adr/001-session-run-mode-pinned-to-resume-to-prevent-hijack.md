---
status: accepted
date: 2026-06-19
decision-makers: Travis Ennis
---
# Session run-mode pinned to --resume to prevent hijack

## Context and Problem Statement

cake-repl drives cake as a subprocess. cake's --continue flag picks the latest session in the current directory, which creates a hijack risk: an unrelated cake process creating a newer session in the same cwd would silently take over the conversation.

## Decision Drivers

- **Security boundary.** A subprocess REPL running in a shared current working directory
  must not allow an unrelated cake invocation to silently take over the
  conversation mid-stream.
- **Predictable behavior.** After the first successful prompt, the session identity
  should stay stable until the user explicitly resets it (`/new`).
- **Testability.** Run-mode transitions must be a pure, I/O-free state machine so
  every path is covered by unit tests.

## Considered Options

### Always use `--continue`

`--continue` tells cake to pick the latest session in the current directory.
Simple, but vulnerable: any cake process that creates a newer session (including
one the user runs from a terminal, a cron job, or another tool) becomes the
REPL's target on the next prompt.

### Always use `--resume <session-id>`

`--resume` targets a specific session UUID. Immune to hijack because new sessions
created elsewhere don't change the pinned ID. The tradeoff is that the REPL must
track the session ID from the first successful task onward.

### Use `--continue` once then pin (hybrid)

Start with `--continue` for the very first prompt, then switch to
`--resume <id>` after a session ID is known. The `--continue` path covers the
still-uncommon case where cake reports success but no session ID (legacy
behavior). Not chosen: adds complexity for an edge case that essentially never
occurs with modern cake.

## Decision Outcome

Chosen option: **always use `--resume <session-id>` once a session ID is
known**, with a single fallback to `--continue` only when a successful task
reported no session ID.

The initial prompt always runs in `RunFresh` mode. Thereafter:

- If a session ID has been announced — by `TaskStart` or by `TaskComplete`,
  regardless of outcome — pin `NextMode` to `RunResume` with `ResumeID =
  SessionID`. All future prompts use `--resume <id>`.
- If `TaskComplete.IsError == false` and no session ID was ever reported
  (legacy fallback), use `RunContinue`. This path is retained for forward
  compatibility but is not the expected happy path.
- If `TaskComplete.IsError == true` and no session ID was ever reported, leave
  the run mode untouched. There is nothing to resume, and `--continue` is the
  hijack vector this ADR exists to close, so a failure must never select it.

A canceled task (the user interrupts with Ctrl-C) preserves the session pin if a
session id was already announced by `TaskStart`. This prevents a canceled run
from silently starting a new session on the next prompt, which would split the
conversation and reintroduce the hijack risk.

**Amended 2026-08-09.** The original decision also declined to advance the run
mode after a *failed* task, on the reasoning that a failure should not cause a
mode switch the user didn't request. That rule was too broad. It was written
against the `RunContinue` fallback — the actual hijack vector — but it also
suppressed the safe `RunResume` transition, so a first prompt that failed left
`NextMode` at `RunFresh` and the user's next prompt silently started a new
session.

Observed in practice: a cake run executed 90 turns and 111 tool calls over
eleven minutes, then died on a provider-side 400. The follow-up prompt spawned
a fresh session with no context, orphaning a complete 420-record session file
cake had already written — including its failing `task_complete`. `--resume`
against that id would have worked.

Failure is the same shape as cancellation, which this ADR already pins: in both
cases cake has written durable, resumable state, and the danger is losing it,
not switching modes. The two paths now converge on `pinToSession`. The
`--continue` fallback remains success-only, so the hijack boundary is unchanged.

User-visible slash commands that override the state machine:

- `/new` resets to `RunFresh`.
- `/resume <id>` explicitly sets `RunResume` with a given ID.
- `/continue` sets `RunContinue`.

### Consequences

- Good, because **hijack is impossible**: once pinned to a session UUID, no
  external cake process can redirect the conversation.
- Good, because **the behavior is testable**: `sessionState` is a pure struct
  with zero I/O. The full transition table is covered in `session_test.go`
  (success-pin, failure-pin, failure-without-session-id-no-advance, cancel-pin,
  fallback to continue, reset, and manual overrides).
- Good, because **a failed run is recoverable**: the work a failing task
  completed stays reachable from the next prompt instead of being orphaned.
- Good, because **it's simple**: the state machine is ~90 lines with eight
  methods (`RunOptions`, `OnTaskStart`, `OnTaskComplete`, `OnCancel`,
  `pinToSession`, `Reset`, `UseContinue`, `UseResume`). No goroutines, no file
  I/O, no locking.
- Neutral, because **the REPL can't recover if the pinned session file is
  deleted**. The next prompt will fail with a cake error, and the user must
  `/new` to start over. This matches the least-surprise behavior: silently
  switching sessions would be worse.
- Neutral, because **the `--continue` fallback path is theoretically reachable
  but essentially dead code** with modern cake. It is retained as a safety net
  rather than removed, since its presence doesn't complicate the state machine
  meaningfully.

## More Information

- Implementation: `internal/app/session.go` (the `sessionState` struct and its
  methods, including `OnCancel` for the cancellation pin).
- Tests: `internal/app/session_test.go` (ten cases covering all transitions
  including cancellation).
- Guardrail: [`docs/guardrails/session-and-security.md`](../guardrails/session-and-security.md)
  defines the compatibility surface and common failure modes.
- Architectural invariant documented in
  [`ARCHITECTURE.md`](../../ARCHITECTURE.md#architectural-invariants).
- The `--continue`/`--resume` flags are part of the
  [cake contract](https://github.com/travisennis/cake); this ADR only governs
  how cake-repl chooses between them.

