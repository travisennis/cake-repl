---
status: accepted
date: 2026-10-12
---
# Cancel and bound the read-only session listing

## Context and Problem Statement

The `/sessions` browser lists this directory's sessions by running the read-only
`cake sessions list --json` command off the Update loop (ADR 016). Before this
decision the model handed that listing `context.Background()`, kept no cancel
function or request identity, and `ListSessions` set no `WaitDelay`. Three
consequences followed:

- Closing the browser (`Esc`, `q`, `Ctrl+C`) or exiting could not stop an
  in-flight listing; the helper ran to completion with no consumer.
- Closing and reopening launched independent listings, and a late result from an
  older request overwrote the current listing or error and cleared its loading
  state. `sessionsLoadedMsg` carried no generation, and every result was applied.
- A helper that ignored termination, or a descendant that inherited the stdout
  pipe, could hang the listing with no model-owned cancellation path.

The fix must not cross the engine boundary or pre-empt the descendant/terminal
policy owned by task 093.

## Decision Drivers

- Make the listing a model-owned operation that can be canceled on close,
  replacement, and shutdown.
- Ensure a superseded result can never alter the current request's items, error,
  selection, or loading state.
- Bound completion so the listing cannot hang indefinitely on a stuck or
  pipe-holding helper.
- Preserve the existing read-only invocation contract (ADR 016): 8 MiB stdout
  cap, bounded stderr tail, non-fatal failure, cwd, debug-log synchronization.
- Keep the boundary with task 093 explicit: no terminal/controlling-terminal
  isolation and no descendant reaping are added here.

## Considered Options

- Leave the listing uncancelable: rejected. The accepted finding (LIFECYCLE-13)
  is precisely that close/exit cannot stop it and a stale result can clobber the
  current view.
- Cancel on close only: rejected. A close-then-reopen still needs the model to
  ignore the first request's late result.
- Model-owned cancellable listing keyed by a monotonic request identity, plus a
  bounded `WaitDelay` in `ListSessions`: chosen.
- Implement full process-group/descendant isolation here: rejected. That is task
  093's decision, not this task's, and it must not be guessed.

## Decision Outcome

The model owns one in-flight listing, with a monotonic request identity.

- A monotonic counter gives each listing request an identity that never repeats.
  `sessionsLoadedMsg` carries it, and the handler applies a result only while its
  identity matches the active request; a superseded or invalidated result is
  dropped without touching items, error, selection, or loading state.
- The model stores the active request's `context.CancelFunc`. Closing the
  browser, starting a replacement listing, and quitting (every `tea.Quit` the
  model reaches) cancel and invalidate it.
- `ListSessions` sets `cmd.WaitDelay` to 3 seconds, matching the live-run SIGTERM
  grace window. `exec.CommandContext`'s default kill stops the read-only helper
  on cancellation, and `WaitDelay` bounds Wait when a child fails to exit or when
  a descendant still holds the stdout/stderr pipes. Cancellation is a hard kill,
  not SIGTERM: the listing is read-only and holds no state to flush.

The engine contract is unchanged: `cake sessions list --json`, read-only, run in
the same working directory as a live prompt, 8 MiB stdout cap, bounded stderr
tail, and non-fatal failure. Terminal/controlling-terminal isolation and
descendant reaping remain out of scope and owned by task 093.

### Consequences

- Good: close, replace, and shutdown stop the helper, and a late result cannot
  touch the browser.
- Good: completion is bounded by `WaitDelay` even when a descendant holds stdout;
  the failure degrades to the non-fatal "sessions unavailable" warning.
- Good: prompts stay available after the browser closes, and selection is
  restricted to a row of the current successful listing.
- Neutral: a helper that exits cleanly but leaves a pipe open makes `WaitDelay`
  return `ErrWaitDelay`, which the browser renders as the same non-fatal warning;
  the user can reopen and retry.
- Neutral: without task 093, cake-repl signals only the direct child and closes
  the pipes; a killed helper's own descendants are not reaped by cake-repl.

## More Information

- Task 106: give session listings cancellation and request identity.
- [ADR 016](016-allow-read-only-cake-sessions-list-for-the-session-browser.md):
  the read-only listing boundary this decision stays within.
- [ADR 011](011-replay-resumed-sessions-through-cake-stream-json.md): the
  precedent for a read-only cake command with non-fatal failure.
- [`../guardrails/session-and-security.md`](../guardrails/session-and-security.md)
- [`../guardrails/cake-integration-and-stream-json.md`](../guardrails/cake-integration-and-stream-json.md)
- Related: task 093 owns terminal isolation and descendant policy.
