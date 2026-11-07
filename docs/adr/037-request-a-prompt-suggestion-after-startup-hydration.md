---
status: accepted
date: 2026-11-07
decision-makers: Travis Ennis
---
# Request a prompt suggestion after startup hydration

## Context and Problem Statement

ADR 028 starts a suggestion only after a live turn delivers a clean completion
record. A REPL started with `-resume <uuid>` hydrates the visible timeline from
cake's read-only replay, but hydration does not set the live run-outcome state, so
`completedTurn` is false and no suggestion is requested until the user completes
a new turn. That leaves the first prompt after a resume without the hint even
though the resumed conversation already ends at a completed turn.

The replay is bounded and already fully consumed before the prompt gate opens.
Requesting a suggestion after it settles spends the same one read-only
`cake-suggest` call the feature already makes after a live turn, with no new
session access or subprocess contract.

## Decision Drivers

- The first prompt after a resume should have the same hint as the next prompt
  after a live turn.
- A suggestion must not be requested from a session whose last task failed,
  was canceled, or never completed.
- Replay hydration is read-only and finishes before the request; the suggester
  must not race the hydration child or the session pin.
- The startup path must not accidentally enable between-turn compaction or any
  other operation that treats a live completed turn as its trigger.
- The feature remains opt-in, one-shot, and silent on no-op.

## Considered Options

- **Request after every startup replay (chosen, with a clean-completion gate).**
  The replay result supplies the completed-turn evidence; one suggestion starts
  after the replay child is reaped.
- **Request at process start before hydration.** Rejected: it races the replay
  and can read a session while the visible timeline is still empty or wrong.
- **Continue waiting for the first live turn.** Rejected: it preserves the gap
  this decision exists to close.
- **Hydrate through a second replay specifically for the suggester.** Rejected:
  `cake-suggest` already replays the session itself; a second REPL replay adds
  no information.

## Decision Outcome

Chosen option: after a clean startup `-resume <uuid>` hydration that ends at a
completed, non-errored turn, start one suggestion request through the existing
`maybeStartSuggestion` path.

- **Eligibility.** The replay must be current, uncanceled, exit zero, have no
  replay error or start warning, and end with `sawComplete` true and
  `session.LastComplete` non-nil and not `is_error`. A replay `task_start`
  clears the completion evidence so a session that ends mid-task does not
  qualify on an earlier turn's completion.
- **Turn evidence.** A qualifying replay sets a suggestion-only eligibility
  flag; it does not write the live run outcome or enable between-turn
  compaction. The suggestion is then subject to every existing gate: enabled
  config, a pinned `RunResume` session, an empty composer, idle REPL, and no
  request already in flight.
- **Timing.** The request starts only after the replay result has settled and its
  child has been reaped. It runs alongside no compaction, because startup has no
  just-finished live turn to compact.
- **Scope.** Only a hydrated `-resume <uuid>` startup qualifies. A bare
  `-resume` that opens the session picker hydrates nothing, and a picked row
  pins the next prompt without replaying it; those paths keep the existing
  no-suggestion-until-first-turn behavior.
- **Failure.** A failed, canceled, incomplete, or warned replay leaves the
  startup state exactly as it is today: no suggestion, and the resume pin stays
  usable.

### Consequences

- Good, because a resumed session offers a next prompt immediately, matching
  the after-turn experience.
- Good, because the suggestion is evidence-gated and cannot be based on a
  half-written or failed session.
- Bad, because enabling suggestions now spends one metered call at startup as
  well as after each completed turn.
- Neutral, because the request is still one-shot, read-only, and silent on
  `suggested:false`.

## More Information

- Extends ADR 028's trigger clause; acceptance and restoration are changed by
  ADR 036.
- The replay completion evidence is derived from the same stream-json records
  used for timeline hydration. The suggester still reads the session itself;
  cake-repl never reads session files.
