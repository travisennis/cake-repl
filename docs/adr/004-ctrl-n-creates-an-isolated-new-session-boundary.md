---
status: accepted
date: 2026-07-19
decision-makers: Travis Ennis
---
# Ctrl+N creates an isolated new session boundary

## Context and Problem Statement

The REPL can select a fresh cake session with `/new`, but users cannot begin a visibly clean conversation from the keyboard. A new-session shortcut must work while idle or while cake is running without allowing late events or cancellation bookkeeping from the old run to restore the old session pin or pollute the new timeline.

## Decision Drivers

- `Ctrl+N` should provide a conventional, immediate new-session action.
- An active cake subprocess must still follow the existing graceful cancellation lifecycle.
- The prior session's events must not appear after the new-session boundary.
- Prompt history and startup model/profile settings must survive the boundary.
- The next submitted prompt must use fresh mode with no old session identifier.

## Considered Options

1. Make `Ctrl+N` an alias for `/new`. This resets session targeting but leaves the old timeline visible and does not handle a running task as a complete transition.
2. Reset immediately and continue processing the old run's remaining events. This feels immediate but can mix old output into the new timeline and allow cancellation state to re-pin the old session.
3. Clear the conversation view immediately, cancel an active run through the existing runner, ignore its remaining typed events, and keep fresh session state after its terminal result. This preserves process safety and creates an unambiguous boundary.

## Decision Outcome

Chosen option: 3, because it gives users an immediate visible transition while retaining the established subprocess cancellation and session-hijack protections.

`Ctrl+N` resets local session state to fresh, clears the timeline, clears obsolete pending tool-call bookkeeping, and adds a `New session` informational item. Prompt history, the current input draft, model/profile configuration, working directory, and tool-output display mode remain unchanged. If a cake run is active, the shortcut requests cancellation and the app drains the run to completion while suppressing any subsequent events from that old run. The old run's cancellation result must not call `sessionState.OnCancel`, because doing so would restore the old session pin across the explicit boundary.

No confirmation or session-summary write is performed. cake remains the owner of session persistence, and cake-repl continues not to read or write cake session files.

### Consequences

- Good, because the next prompt reliably starts fresh even when the shortcut interrupted an active run.
- Good, because late stream events cannot contaminate the new timeline.
- Good, because history and stable startup settings remain available.
- Neutral, because the model needs transient state while it drains a canceled old run.
- Neutral, because `Ctrl+N` becomes a documented compatibility surface alongside the existing key bindings.

## More Information

- Task: [035](../../.ahm/tasks/completed/035.md)
- Guardrails: [`session-and-security.md`](../guardrails/session-and-security.md), [`cli-and-user-output.md`](../guardrails/cli-and-user-output.md)
