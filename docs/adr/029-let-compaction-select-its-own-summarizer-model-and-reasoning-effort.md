---
status: accepted
date: 2026-10-28
decision-makers: Travis Ennis
---
# Let compaction select its own summarizer model and reasoning effort

## Context

The REPL's between-turn compactor currently passes the turn's `-model` and
`-profile` to `cake-compact`. That is a safe default, but it couples summary
cost and latency to the model chosen for coding turns. The summary is a
separate, metered call whose output is dominated by hidden reasoning; users
need to test a cheaper model or a lower reasoning effort without changing the
turn selection. `cake-compact` already accepts `-summarizer-model` and
`-summarizer-profile`; it also accepts `-summarizer-reasoning-effort` after
this decision.

## Decision

`[compaction]` gains three trusted-config keys:

```toml
[compaction]
summarizer-model = "deepseek-flash-max"
summarizer-profile = "summary"
summarizer-reasoning-effort = "low"
```

- `summarizer-model` and `summarizer-profile` each override the corresponding
  turn selection independently. An unset value falls back to the REPL's
  `-model` / `-profile`, preserving the current behavior.
- `summarizer-reasoning-effort` is passed through when set. An unset value
  leaves Cake's own reasoning default in effect.
- cake-repl passes the resolved values to `cake-compact compact` as
  `-summarizer-model`, `-summarizer-profile`, and
  `-summarizer-reasoning-effort`. The compactor's threshold, in-place append,
  and result contract are unchanged.
- The automatic project-local `.cake-repl.toml` layer ignores all three keys
  with a startup warning. They may only come from XDG config or an explicit
  `-config`, because they select a metered operation's cost and behavior. This
  extends ADR 024's provenance rule.

## Consequences

- Good: a user can evaluate a faster or cheaper summary configuration without
  changing the coding model, and can lower reasoning effort directly instead of
  creating a profile.
- Good: the existing turn-model fallback keeps current configs unchanged.
- Bad: another set of keys to keep aligned across the README, ADRs, config
  validation, and the compactor CLI.
- Neutral: the summary's quality and cost still require explicit evaluation;
  this decision adds selection, not a policy or a default change.

## More Information

- [ADR 023](023-compact-context-between-turns-through-an-external-compactor.md)
  establishes the compactor contract and the current turn-model fallback.
- [ADR 024](024-ignore-project-local-compaction-tuning.md) defines the
  project-local provenance rule extended here.
- [ADR 025](025-add-an-absolute-context-trigger-with-a-separate-quality-ceiling.md)
  defines the absolute trigger this selection sits beside.
- `cake-compact`'s `docs/latency-deep-dive.md` explains why reasoning effort is
  the first latency lever.
