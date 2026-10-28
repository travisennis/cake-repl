---
status: accepted
date: 2026-10-28
decision-makers: Travis Ennis
---
# Let prompt suggestions select reasoning effort

## Context and Problem Statement

ADR 030 gave `[suggestions]` trusted `model` and `profile` keys but deferred
`reasoning-effort` because `cake-suggest` did not accept it. The suggester now
accepts `-reasoning-effort` and forwards it to its isolated Cake completion.
Without a REPL key, a user can select a cheaper suggestion model but cannot
lower the reasoning effort that dominates a small suggestion call's cost and
latency. `[compaction]` already has this control through ADR 029.

## Decision Drivers

- Parity with the table-scoped summarizer selection in `[compaction]`.
- A suggestion is a metered call made after every completed turn, so its
  reasoning effort should be tunable without changing the coding model.
- Existing configs must keep their behavior; an unset value must omit the flag.
- The automatic project-local layer must not be able to spend more on a metered
  operation.

## Considered Options

- **Keep only model/profile selection.** Rejected: a reasoning-heavy model can
  still make the suggestion call slower and more expensive than necessary.
- **Reuse a turn-level reasoning setting.** Rejected: cake-repl has no
  `-reasoning-effort` turn flag to fall back to, and the suggestion is a
  separate operation.
- **Add `[suggestions] reasoning-effort` and pass it only when set (chosen).**
  It matches the compaction pattern and keeps Cake's default when unset.

## Decision Outcome

Chosen option: add one trusted-config key and pass it through only when set.

```toml
[suggestions]
reasoning-effort = "low"
```

- The accepted values are `none`, `low`, `medium`, `high`, `xhigh`, and `max`,
  matching Cake's CLI enum and `[compaction]`.
- cake-repl passes the value to `cake-suggest suggest` as
  `-reasoning-effort <value>`. An unset value omits the flag, so Cake's own
  reasoning default remains in effect.
- `model` and `profile` keep their independent fallback to the REPL's
  `-model` and `-profile`. `reasoning-effort` has no turn fallback.
- The automatic project-local `.cake-repl.toml` layer ignores the key with a
  startup warning. It may only come from XDG config or an explicit `-config`,
  extending ADR 024's provenance rule.
- This partially supersedes ADR 030's clause that deferred `reasoning-effort`.
  ADR 030's model/profile resolution rule is unchanged.

### Consequences

- Good: the suggestion call can use a lower reasoning effort without changing
  the coding turn's model or profile, closing the parity gap with compaction.
- Good: an unset key emits no flag, so existing configs and older suggester
  binaries keep working unless the user opts in.
- Bad: the key requires a `cake-suggest` build that accepts
  `-reasoning-effort`; an older binary rejects the invocation and the REPL
  surfaces its usual one-time suggestion warning.
- Bad: another config key to mirror across README, validation, tests, and the
  suggester invocation.
- Neutral: this adds selection, not a default or a quality policy. Suggestion
  cost and quality still require explicit evaluation.

## More Information

- [ADR 030](030-let-prompt-suggestions-select-their-own-model-and-profile.md)
  establishes the model/profile selection and is partially superseded here.
- [ADR 029](029-let-compaction-select-its-own-summarizer-model-and-reasoning-effort.md)
  establishes the table-scoped reasoning-effort pattern this decision follows.
- [ADR 024](024-ignore-project-local-compaction-tuning.md) defines the
  provenance rule extended here.
- `cake-suggest`'s `-reasoning-effort` flag is delivered in that repository and
  documented in its `README.md` and ADR 001.
