---
status: accepted
date: 2026-10-24
decision-makers: Travis Ennis
---
# Ignore project-local compaction tuning

## Context and Problem Statement

ADR 023 makes between-turn compaction opt-in through `[compaction]` and treats
the automatically loaded project-local `.cake-repl.toml` as untrusted
provenance: the project layer cannot enable compaction or select
`cake-compact-bin`, but it may tune `threshold-percent` and `keep-tokens`.

That tuning is an amplification path. Once compaction is enabled in XDG config
or an explicit `-config` file, a cloned repository can set
`threshold-percent = 1` and force a `cake-compact` run — a metered summarizer
call and a session append — after nearly every successful turn. The untrusted
layer still cannot turn the feature on, but it can turn an enabled feature into
near-continuous spending and retirement. ADR 023 recorded this as accepted
residual risk; this decision removes it.

## Decision Drivers

- The project-local layer must not control how often a metered,
  session-mutating operation runs.
- A cloned repository must not be able to amplify cost after the user enables
  the feature globally.
- The rule should be simple and match the existing executable/enable provenance
  rule: untrusted config can turn an operation off, never on or up.
- Trusted config (XDG or an explicit `-config`) remains sufficient for tuning.

## Considered Options

- Keep project-local tuning and rely on the documented residual risk: rejected;
  a one-line repo file could force near-every-turn compaction.
- Floor project-local threshold values: rejected; any floor is arbitrary and
  still lets an untrusted file move the policy toward more spending.
- Ignore all project-local compaction keys except `enabled = false`: chosen; it
  matches ADR 006/023's provenance boundary and keeps the rule easy to state.

## Decision Outcome

Chosen option: ignore project-local tuning, because the automatic project-local
`.cake-repl.toml` must not influence how often an enabled metered operation runs.

`LoadProject` ignores `compaction.threshold-percent` and
`compaction.keep-tokens` with a startup warning naming the file and key, just as
it ignores `compaction.enabled = true` and `compaction.cake-compact-bin`. Key
presence is matched case-insensitively, as the TOML decoder matches struct
fields, so a differently spelled key (`[Compaction] Threshold-Percent = 1`)
cannot bypass the rule. The only compaction key the automatic project-local
layer may set is `enabled = false`, which can disable the feature for that
project. Trusted XDG config and an explicit `-config` file supply
`threshold-percent` and `keep-tokens`.

This partially supersedes ADR 023's provenance bullet. ADR 023 remains accepted
for the trigger, in-place checkpoint, gating, and result semantics; its
project-local tuning allowance and the residual-risk bullet no longer apply.

### Consequences

- Good, because a cloned repository can no longer amplify an already-enabled
  compaction into near-every-turn spending or aggressive retirement.
- Good, because the provenance rule is uniform: the untrusted layer can turn
  compaction off but cannot turn it on, select its executable, or tune it.
- Bad, because a project cannot set a project-specific threshold or retained
  tail; that policy now lives only in trusted config.
- Neutral, because disabling compaction per project remains supported.

## More Information

- [ADR 023](023-compact-context-between-turns-through-an-external-compactor.md)
  is partially superseded: its project-local tuning allowance and residual-risk
  bullet are replaced by this decision.
- [ADR 006](006-project-local-config-cannot-select-the-cake-executable.md)
  defines the untrusted-provenance rule this decision extends.
- [ADR 025](025-add-an-absolute-context-trigger-with-a-separate-quality-ceiling.md)
  extends the ignored tuning keys to `max-context-tokens` and
  `trigger-tokens`; the rule that the untrusted layer may only disable
  compaction is unchanged.
- cake-repl task 119 (opt-in between-turn compaction) is the implementation.
