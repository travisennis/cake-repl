---
status: accepted
date: 2026-10-02
decision-makers: Travis Ennis
---
# Source the status-line model from the cake stream

## Context and Problem Statement

The status line renders `m.cfg.Model`, so it names a model only when `--model`
(or config `model`) was supplied. cake resolves a concrete model for every run:
a `[[models]]` entry can be selected by `default_model`, and several entries can
share one provider model ID. cake-repl has never read that resolution, so a
`-resume` start cannot show the resumed session's model, and any run that relies
on cake's default shows an empty or misleading model field.

The resolution is available on the supported CLI boundary. `cake replay` emits
`session_meta` with `model` (the provider model ID) and `model_config` (the
`[[models]]` entry name); cake#664 adds the same pair to the live `task_start`
record. Both are additive optional fields. cake-repl must decide whether the
engine-reported value or the local CLI/config value is authoritative for the
status line.

## Decision Drivers

- The engine is the only source that knows which model cake actually resolved;
  local config can only echo what the user typed.
- `model_config` (the `[[models]]` entry name) is the resolvable identity when
  entries share one provider model ID, so it is preferred over the raw provider
  ID.
- Stay on the public stream-json contract: never read session files or settings
  (ADR 006, ADR 011).
- Decode additively so an older cake — or `cake replay` with an unknown identity
  — still works with no error and no regression.
- The status line already sanitizes cake-supplied text at render, so the new
  value inherits that protection (ADR 005).
- The live path must not require `session_meta` in live output; it waits for the
  additive fields in cake#664.

## Considered Options

1. Keep rendering only the CLI/config value. Rejected: it hides the resolved
   model and can never show a resumed session's model.
2. Resolve the model locally by reading cake's config or session files.
   Rejected: it crosses the engine boundary and duplicates cake's resolution
   rules.
3. Prefer the engine-reported value when present, falling back to the
   CLI/config value, and prefer `model_config` over `model`. Chosen.

## Decision Outcome

Chosen option: 3. The status line's model is the engine-reported value once cake
has reported one, otherwise the CLI/config value.

- Replay hydration takes `session_meta.model_config` and falls back to
  `session_meta.model`, storing it as the displayed model.
- A `task_start` (or `task_complete`) that carries either field updates the
  displayed model the same way, live or replayed, so cake#664 needs no further
  change in cake-repl.
- Resolution order is `model_config`, then `model`, then the CLI/config value.
- The engine-reported value is per session: a new session (`/new`, Ctrl+N) and a
  switch to a different resume target clear it, so the CLI/config value applies
  again until cake reports the new session's model.
- Decoding stays additive. `SessionMeta` is corrected to read cake's real
  `working_directory` field and gains the optional `model_config` field;
  `task_start` and `task_complete` gain optional `model` / `model_config`.
  Missing and unknown fields decode to empty strings, and an older cake that
  reports nothing leaves the status line exactly as it is today.

### Consequences

- Good, because `-resume` shows the resumed session's model and runs using
  cake's default show the model cake actually resolved.
- Good, because the value comes from stream-json records cake-repl already
  decodes; no engine boundary is crossed.
- Good, because cake#664 integrates with no further cake-repl change.
- Neutral, because with a cake older than cake#664 a live run still shows only
  the CLI/config value.
- Bad, because the displayed model can lag a live run until a model-bearing
  record arrives.
- Neutral, because the value is display-only; session pinning and run mode do
  not depend on it.

## More Information

- Task 087, the cake-repl-only half of task 085.
- cake#664: add resolved model identity to live stream-json output.
- [`docs/guardrails/cake-integration-and-stream-json.md`](../guardrails/cake-integration-and-stream-json.md).
- ADR 005 (render-boundary sanitization), ADR 006 (project-local config cannot
  select the cake executable), ADR 011 (replay through stream-json).
