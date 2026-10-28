---
status: accepted
date: 2026-10-28
decision-makers: Travis Ennis
---
# Let prompt suggestions select their own model and profile

## Context

The REPL's opt-in prompt suggester currently passes the turn's `-model` and
`-profile` to `cake-suggest`. That is a safe default, but it couples the cost
and latency of a metered, read-only suggestion call to the model chosen for
coding turns. The compaction summarizer already has its own trusted selection
(ADR 029), and `cake-suggest` already accepts `-model` and `-profile`; only the
REPL configuration and resolution are missing.

## Decision

`[suggestions]` gains two trusted-config keys:

```toml
[suggestions]
enabled = true
cake-suggest-bin = "cake-suggest"
model = "deepseek-flash-low"
profile = "suggestion"
```

- `model` and `profile` each override the corresponding turn selection
  independently. An unset value falls back to the REPL's `-model` / `-profile`,
  preserving current behavior.
- cake-repl passes the resolved values to `cake-suggest suggest` as `-model`
  and `-profile`, omitting either when empty. The suggester's flags and JSON
  contract are unchanged.
- The automatic project-local `.cake-repl.toml` layer ignores both keys with a
  startup warning. They may only come from XDG config or an explicit `-config`,
  because they select a metered operation's cost and provider behavior. This
  extends ADR 024's provenance rule.
- `reasoning-effort` is not added: `cake-suggest` does not accept it. If the
  suggester grows that flag, the same table can expose it without changing this
  resolution rule.

## Consequences

- Good: a user can evaluate a faster or cheaper suggestion model without
  changing the coding model, using the same table-scoped naming as
  `[compaction]`.
- Good: the existing turn-model fallback keeps current configs and behavior
  unchanged.
- Bad: another pair of config keys to mirror across the README, ADRs, config
  validation, and the suggester invocation.
- Neutral: this decision adds selection, not a default or a quality policy.
  Suggestion quality and cost still require explicit evaluation.

## More Information

- [ADR 028](028-offer-after-turn-prompt-suggestions-from-an-external-suggester.md)
  establishes the suggester boundary and the turn-model fallback.
- [ADR 029](029-let-compaction-select-its-own-summarizer-model-and-reasoning-effort.md)
  establishes the table-scoped selection pattern and trusted-config rule this
  decision follows.
- [ADR 024](024-ignore-project-local-compaction-tuning.md) defines the
  provenance rule extended here.
