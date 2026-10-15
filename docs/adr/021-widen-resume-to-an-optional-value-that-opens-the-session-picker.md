---
status: accepted
date: 2026-10-15
decision-makers: Travis Ennis
---
# Widen -resume to an optional value that opens the session picker

## Context and Problem Statement

`-resume <uuid>` hydrates one specific session's visible history at startup, and
the `/sessions` browser lets a user pick a past session once the REPL is
running. There is no way to pick a session while launching: a bare `-resume` is
a flag error (`flag needs an argument: -resume`), so a user who wants to resume
but does not know the UUID must start the REPL, run `/sessions`, and choose
there. Task 011 built the browser and deliberately left this startup gap to a
follow-up (task 091).

The startup CLI already has the pattern the gap needs: `-fork [<uuid>]` accepts
an optional value through the `optionalString` flag type and a normalization
step in `cmd/cake-repl/main.go` that rewrites a bare flag to the standard flag
package's `--flag=` form. `-resume` is the only remaining startup flag whose
value is mandatory.

Widening `-resume` from a required value to an optional one changes a
user-facing CLI contract, so it needs a decision record before implementation.
The relevant precedents are
[ADR 011](011-replay-resumed-sessions-through-cake-stream-json.md) (replay
hydration for a resumed session) and
[ADR 016](016-allow-read-only-cake-sessions-list-for-the-session-browser.md)
(the read-only `cake sessions list --json` source the browser lists from).

## Decision Drivers

- Close the startup gap using the browser and listing contract that already
  exists; add no new data source and no new dependency (`bubbles/list` is
  already used).
- Keep `-resume <uuid>` byte-for-byte behaviorally identical: replay hydration
  plus an explicit `--resume <uuid>` pin for the next prompt.
- Keep one obvious way to name a session at startup; a second flag for the same
  workflow is extra surface a user has to learn and remember.
- Preserve every existing invariant: `-fork` and `-resume` stay mutually
  exclusive, hydration never runs for a pick, and the browser's pick pins the
  next prompt through the same transition as `/resume <uuid>`.
- Stay inside engine isolation: the picker lists through `cake sessions list
  --json` only.

## Considered Options

- **Widen `-resume` to an optional value (chosen).** A bare `-resume` opens the
  session picker at startup and hydrates nothing; `-resume <uuid>` keeps today's
  behavior. The flag keeps one name for "resume something" and mirrors the
  accepted `-fork [<uuid>]` shape.
- **A distinct flag (`-pick` / `-sessions`).** Rejected: it duplicates the same
  workflow under a second name, leaves `-resume` still unable to browse, and
  costs the same documentation and validation surface as widening the flag.
- **Do nothing.** Rejected: the workflow gap is real and the browser already
  provides everything the picker needs.

## Decision Outcome

`-resume` accepts an optional value:

- A bare `-resume` opens the session browser at startup, idle, with no replay
  hydration. Picking a row pins the next prompt through the existing
  `/resume <uuid>` transition (`sessionState.UseResume`) and reports the same
  notice; closing the picker with `Esc`, `q`, or `Ctrl+C` leaves the REPL idle
  and fresh, exactly as if `-resume` had not been given, and the prompt is then
  available. The listing stays a model-owned, bounded, cancellable operation
  (ADR 019) and a listing failure renders the browser's existing non-fatal
  warning state.
- `-resume <uuid>` keeps replay hydration and the explicit `--resume <uuid>`
  pin unchanged (ADR 011).
- `-fork` and `-resume` remain mutually exclusive in every form, bare
  `-resume` included.

The optional value is parsed by the same mechanism `-fork` already uses: the
`optionalString` flag type plus a normalization step generalized to the
optional-value flags, so both flags share one implementation and one set of
tests.

### Consequences

- Good, because a user can resume without knowing a UUID: launch with `-resume`,
  pick a row, and type the first prompt.
- Good, because the change reuses the existing browser, listing, and resume
  transition instead of adding a parallel path, so the session-pinning and
  cancellation invariants stay in one place.
- Good, because the error for a bad value is unchanged: `-resume not-a-uuid`
  still fails validation with `invalid -resume uuid`.
- Neutral, because a bare `-resume` on a directory with no sessions shows the
  browser's empty state and leaves the REPL fresh; there is nothing to resume.
- Neutral, because the picker lists only the REPL's working directory, so a
  session from another directory must still be resumed by UUID.
- Bad, because `-resume` is no longer self-documenting as value-taking: a
  mistyped `-resume <uuid>` becomes a bare `-resume` if the value is missing or
  starts with `-`, so the REPL opens the picker instead of erroring. This
  mirrors the existing `-fork` behavior and is visible on screen.
- Bad, because the exit-hint and flag-validation tests, plus `README.md` and two
  guardrails, move to the optional-value form.

## More Information

- Task 091: let a bare `-resume` open the session picker at startup (follow-up
  to task 011's browser).
- [ADR 011](011-replay-resumed-sessions-through-cake-stream-json.md): replay
  hydration for `-resume <uuid>`; unchanged by this decision.
- [ADR 016](016-allow-read-only-cake-sessions-list-for-the-session-browser.md):
  the read-only `sessions list --json` contract the picker lists through.
- [ADR 019](019-cancel-and-bound-the-read-only-session-listing.md): the listing
  is model-owned, cancellable, and bounded; the startup picker uses that same
  operation.
- [`../guardrails/cli-and-user-output.md`](../guardrails/cli-and-user-output.md)
  and
  [`../guardrails/session-and-security.md`](../guardrails/session-and-security.md):
  the flag and session-behavior surfaces this decision moves.
- Implementation: `cmd/cake-repl/main.go`, `internal/app/model.go`,
  `internal/app/update.go`, `internal/app/sessions.go`.
