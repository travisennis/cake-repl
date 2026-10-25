---
status: accepted
date: 2026-10-25
decision-makers: Travis Ennis
---
# Add an absolute context trigger with a separate quality ceiling

## Context and Problem Statement

Between-turn compaction (ADR 023) fires at a five-point headroom below
`threshold-percent` of the model window. On a 1M-token model that is a 750k
line, and the Phase 5 free pass found no real session reached even 57% of the
window: the trigger has never fired. Trav set a 400k-token quality ceiling for
coding agents because latency and attention degrade above it. That ceiling is
deliberately independent of the model window; it is a quality policy, not a
capacity limit. The percent-shaped config cannot express it, and the
compactor's `-threshold-tokens` flag is unused.

## Decision Drivers

- The trigger must be expressible as an absolute token line below an explicit
  quality ceiling.
- The ceiling is policy evidence, not the compaction line. Passing it to the
  compactor would make the compactor decline a compaction the REPL already
  decided to make.
- Existing percent configs must keep working exactly as before; the feature
  stays opt-in and experimental.
- The model window can be smaller than the ceiling; the trigger must never
  exceed the window.
- Project-local config remains untrusted: it may only disable compaction,
  never enable, select, or tune it.
- cake-repl must not read session files; the compactor re-checks the session.

## Considered Options

- **Replace `threshold-percent` with absolute tokens.** Rejected: it breaks
  old configs and removes a useful window-relative fallback.
- **Keep percent only and document a token conversion.** Rejected: the ceiling
  is quality policy independent of the window; a 400k ceiling on a 1M window
  is not 40%.
- **Add absolute fields beside the percent fallback, with zero meaning
  unset (chosen).** It preserves old behavior and makes the new policy
  explicit and tunable.

## Decision Outcome

Chosen option: add an absolute trigger and ceiling while keeping the percent
fallback, because the 400k quality policy is not a window percentage and old
configs must not change.

`[compaction]` gains:

```toml
max-context-tokens = 400000  # quality ceiling; recommended starting value
trigger-tokens = 350000      # compaction line; recommended starting value
threshold-percent = 80       # percent fallback, only when the absolute fields are unset
keep-tokens = 20000
```

- **Mode selection.** Absolute mode is selected only when
  `max-context-tokens` or `trigger-tokens` is non-zero. Both must then be
  positive and `trigger-tokens < max-context-tokens`; otherwise startup fails
  with a config error naming the field. Zero for both means percent mode and
  preserves the old 80% default and five-point headroom.
- **Starting values.** 400000/350000 are documented recommended starting
  values, not hard-coded defaults. No config change is required for existing
  percent users.
- **Trigger.** In absolute mode the REPL fires when the newest complete
  `turn_usage` input figure is at least the effective trigger. It does not
  subtract a percentage headroom. The effective trigger is `trigger-tokens`,
  capped at 75% of the model window when the window is smaller than the
  ceiling; the cap is documented and never set above the window. The 75% cap
  matches the default percent mode's effective line (80 − 5).
- **Compactor call.** The compactor is invoked with exactly one threshold
  flag: `-threshold-tokens <effective-trigger>` in absolute mode,
  `-threshold-percent <threshold-percent>` in percent mode. The ceiling is
  never passed. The compactor still re-checks the session and may return
  `compacted:false`; `-force` and `-fork` remain forbidden.
- **Provenance.** ADR 024's rule is unchanged and extends to the new keys: the
  automatically loaded project-local `.cake-repl.toml` cannot enable
  compaction, choose the executable, or tune any threshold, including
  `max-context-tokens` and `trigger-tokens`. It may only set
  `enabled = false`. XDG config or an explicit `-config` may tune.
- **Status estimate.** `after_tokens` remains an estimate for the status line.
  It excludes fixed request overhead, so the first post-compaction status can
  read lower than the next provider input; a later complete `turn_usage`
  replaces it.
- **Qualification.** The feature remains experimental until a live trigger
  validation; this decision adds the policy surface, not a new fidelity pass.

### Consequences

- Good, because a 400k quality ceiling and a 350k trigger are expressible,
  tunable, and reachable on a 1M window.
- Good, because old percent configs behave exactly as before; absolute mode is
  opt-in.
- Good, because the ceiling and trigger can move independently as evidence
  accumulates.
- Bad, because there are two more config keys and a second trigger mode to
  test.
- Bad, because when the window is smaller than the ceiling the effective
  trigger is not the configured trigger; users must read the documented cap.
- Neutral, because the compactor contract is unchanged; it already accepts
  `-threshold-tokens`.

## More Information

- [ADR 023](023-compact-context-between-turns-through-an-external-compactor.md)
  introduced between-turn compaction. This ADR extends its trigger policy and
  leaves its in-place, gating, and result semantics unchanged.
  [ADR 024](024-ignore-project-local-compaction-tuning.md)'s provenance rule is
  extended to the new keys.
- Phase 5 evidence:
  `research/context-compaction-phase5-results.md` ("Addendum: 400k token cap")
  and `research/context-compaction-phase5-instrumentation.md` (Appendix A). At
  a 400k cap: 11 boundaries in 5 sessions (10 viable); 350k: 19/8 (17 viable);
  375k: 14/5 (13 viable).
- cake-repl task 122; cake-compact already supports `-threshold-tokens`.
