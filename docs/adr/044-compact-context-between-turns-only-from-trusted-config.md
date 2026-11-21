---
status: accepted
date: 2026-11-20
---
# Compact context between turns only from trusted config

## Context and Problem Statement

`cake-compact` is an external, metered process that mutates the pinned session
in place. Running it at the wrong time can race cake's session lock, and running
it from a project-local config would let a repository spend the user's quota or
change a metered summarizer. The REPL also has to choose between an absolute
token trigger and the legacy window percentage without passing two policies.

## Decision

- Compaction is opt-in and only trusted config (XDG or an explicit `-config`)
  may enable it, select its executable, tune its trigger, or choose its
  summarizer model, profile, or reasoning effort. The automatic project-local
  layer may only disable it. See [ADR 046](046-keep-project-local-config-disable-only.md).
- A forced compaction after every turn is never the policy. The REPL starts
  compaction only after a clean completed turn, after the session pin and the
  turn's measurement are settled.
- Absolute mode requires both `max-context-tokens` and `trigger-tokens`, with
  the trigger below the ceiling. An incomplete absolute config disables the
  trigger; it does not silently fall back to a percentage. The ceiling is policy
  for the quality target, is never passed as the trigger, and is never passed to
  the compactor at all.
- When the model window is smaller than the ceiling, the effective trigger is
  capped at the percent-mode effective line (80% minus the five-point headroom),
  so it can never exceed the window.
- Percent mode fires five points below the configured threshold. A complete
  `turn_usage` describes the prompt the model saw, not the response and tool
  results that follow it, so triggering early lets the compactor's fuller
  session estimate cross its real threshold. The compactor remains the
  authority and may return `compacted:false`.
- Exactly one threshold mode reaches the compactor: `-threshold-tokens` when
  configured, otherwise `-threshold-percent`. Passing both would obscure which
  policy the REPL used.
- The compactor is never run with `-fork`. The session id stays pinned. An
  `in_place:false` result or a different session id is rejected; the REPL keeps
  the current session and reports the mismatch instead of retargeting.
- The composer and retarget gates stay closed until the child is reaped. Ctrl+C
  cancels the operation through its model-owned context.
- A success resolves the start notice into one shared success marker and seeds
  the status estimate from the compactor's `after_tokens` and, when reported,
  the window it used. A no-op resolves into an explicit skip; a failure,
  cancellation, or non-in-place result keeps the session and figure and warns.

## Consequences

- Good: a project cannot silently spend quota or mutate a session.
- Good: the next prompt cannot race the compactor's session lock.
- Good: the status figure updates immediately after a successful compaction.
- Bad: two trigger policies exist, and the absolute one has an extra capping
  rule for small windows.
- Neutral: the compactor can still decline after the REPL decided to run it.

## More Information

- [`internal/app/compaction.go`](../../internal/app/compaction.go)
- [`internal/compact`](../../internal/compact)
- [`internal/config`](../../internal/config)
