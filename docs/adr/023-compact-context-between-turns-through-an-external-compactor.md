---
status: accepted
date: 2026-10-24
decision-makers: Travis Ennis
---
# Compact context between turns through an external compactor

## Context and Problem Statement

cake-repl spawns one cake process per prompt and pins the next prompt with
`--resume`, so a long conversation keeps resending a growing transcript until it
no longer fits the model's context window. Cake streams the newest complete
main-agent `turn_usage` record (input tokens plus `context_window`), and the
REPL already renders that figure in the status line, but it has no action to
take when the figure approaches the window.

Cake now has a durable between-turn mechanism: `cake sessions checkpoint <uuid>`
appends a format-4 checkpoint that retires a prefix from provider requests while
leaving raw history in the session file (Cake ADR 047, task 127). The local
`cake-compact` proof of concept wraps that verb: it reads the session, asks a
model to summarize a retired prefix, appends the checkpoint in place, and
returns a machine-readable result. Its minimal-fork mode is an escape hatch, not
the integration path.

The REPL has to decide whether it may run that external compactor, when it may
run it, and how a result changes what the user sees. It must do so without
reading cake session files (the engine boundary in ADR 001/011) and without
enabling a metered, session-mutating operation for a cloned repository's user.

## Decision Drivers

- Long sessions need relief without leaving the REPL or changing the session id.
- The feature must be opt-in and disabled by default; it spends model quota and
  writes to the session file.
- cake-repl must not read cake session files or duplicate Cake's projector. The
  compactor owns the session protocol; the REPL owns the trigger and the UI.
- The project-local `.cake-repl.toml` layer is untrusted provenance (ADR 006):
  it must not be able to turn on a metered operation or choose the executable
  that runs it.
- A compaction must never race the next prompt's session lock, and a failure
  must leave the previous session usable.
- Prompts are the only input; nothing may block the single Update loop while a
  compactor runs.

## Considered Options

- **REPL-internal compaction (read the session file and rewrite it).** Rejected:
  it crosses the engine boundary, duplicates Cake's checkpoint format and
  projection rules, and would need a session-file reader the project forbids.
- **Ask cake itself to compact from inside the invocation.** Out of scope: this
  decision is the between-turn path. Mid-invocation compaction is a separate
  Cake decision and would need a request-boundary seam.
- **External `cake-compact` subprocess, in-place, triggered between turns
  (chosen).** It reuses the supported append verb and keeps Cake's agent loop
  and this REPL simple. The REPL supplies the session id and its own live
  measurement; the compactor re-checks from the session and appends only when it
  agrees.

## Decision Outcome

The REPL gains an opt-in `[compaction]` config section:

```toml
[compaction]
enabled = false
cake-compact-bin = "cake-compact"
threshold-percent = 80
keep-tokens = 20000
```

Behavior:

- **Trigger.** After a task completes successfully, the REPL checks its newest
  complete `turn_usage` context figure. When the session has a known id, a
  positive window, and the figure reaches a small headroom below
  `threshold-percent` of the window, the REPL starts `cake-compact compact
  -threshold -json` with the threshold, keep target, and the configured
  summarizer model (when the REPL has one). The headroom compensates for the
  response and tool results that the measurement does not include. The
  compactor re-checks the session and decides whether to act.
- **External boundary.** The compactor is another documented external
  subprocess, invoked only from `internal/compact` with a fixed argument shape.
  Its result is decoded forward-compatibly from one JSON document. The REPL
  never reads or writes a session file and never imports cake-compact's
  internals. It does not pass `-fork`, and it treats a result with `in_place`
  false as a failure rather than accepting a new session id.
- **Success.** The checkpoint is in place, so the session pin (`ResumeID`) does
  not change. The REPL appends one timeline notice through the shared
  `compactionMarker` formatter and seeds the status context from `after_tokens`,
  marked `Estimated` so it renders with `~`. A later complete `turn_usage`
  replaces the estimate. Replay of the same session already renders the
  checkpoint marker (task 120); the live path does not add a second replay
  marker.
- **Gating.** At most one compaction runs per REPL. While it runs, prompt
  submission is rejected with a warning, the status line and title show the
  compacting state, and session-target changes (`/new`, `/resume`, `/sessions`)
  are refused so the compactor cannot race or target the wrong session. Ctrl+C
  and quitting cancel the child; `/exit` cancels and then exits. The canceled
  child is reaped before the REPL quits.
- **Failure, cancellation, or no-op.** The previous session and status figure
  are preserved. A `compacted:false` no-op is silent (the compactor re-checks
  every eligible turn). An operational failure, a missing binary or verb
  (`cake sessions checkpoint is unavailable; upgrade cake`), a locked session,
  or an unexpected `in_place` result produces one visible warning and keeps the
  session usable.
- **Scope exclusions.** `-no-session` disables compaction with a startup
  warning. Two REPL instances may race a stale plan; Cake's append lock
  serializes writers, so the loser surfaces a warning and keeps its session. The
  feature does not cover a single long invocation that fills the window before
  the REPL regains control.
- **Provenance.** The automatically loaded project-local `.cake-repl.toml`
  cannot enable compaction (`enabled = true` is ignored with a startup warning)
  and cannot select `cake-compact-bin` (ignored like `cake-bin`). It may set
  `enabled = false` and tune `threshold-percent` / `keep-tokens` for a project.
  XDG config, an explicit `-config`, and no CLI flag (for now) are the trusted
  sources.

The feature stays experimental and opt-in. Qualification of trigger quality,
summary fidelity, retained selection, and cost is a separate evaluation; until
it passes, the README describes the feature as experimental and makes no
quality claim.

## Consequences

- Good: sessions can continue past the window without a new session id, raw
  history stays available to replay/resume/fork, and Cake's core stays unaware
  of the policy.
- Good: the engine boundary is preserved — the compactor is an external CLI with
  a JSON contract, not a session-file reader.
- Good: a failed or canceled compaction cannot corrupt or retarget the session;
  the worst case is a visible warning and the old context figure.
- Cost: there is now a second required executable (`cake-compact`) for users who
  opt in, plus a version prerequisite (`cake sessions checkpoint`).
- Cost: compaction can only happen between prompts. A single long tool loop can
  still exhaust the window; that gap is out of scope here.
- Compatibility: once a checkpoint is appended, a Cake build without checkpoint
  support cannot read that session. Raw history remains intact, and a future
  recovery fork can produce a checkpoint-free copy.
- Rollback: set `enabled = false` (the default) or remove the section; no
  session state is introduced by the REPL itself.
- Residual risk: once a trusted layer enables compaction, a project-local file
  can tune `threshold-percent` low and `keep-tokens` small, increasing how often
  the compactor runs and how much it retires. This was accepted because the
  metered operation must already be enabled in XDG config or an explicit
  `-config`, and the user can disable it there or in the project; the compactor
  still re-checks the session and its minimum-reduction guard. Flooring or
  ignoring project-local tuning is a separate decision; ADR 024 makes that
  decision by ignoring project-local tuning.

## More Information

- Cake: task 127 (`cake sessions checkpoint`), PR #749 / `5e0ffe8`;
  `docs/integrations.md` "Checkpoint append"; ADR 047 (checkpoint record and
  projector); ADR 048 (turn usage).
- cake-repl: task 120 (replay rendering, PR #2) and task 119 (this
  integration). The workspace initiative `context-compaction-mod` holds the
  cross-project plan and the Phase 5 qualification criteria.
- ADR 006 (project-local executable provenance) is extended, not superseded:
  its rule now covers the compactor executable and the enabling decision.
- [ADR 024](024-ignore-project-local-compaction-tuning.md) partially supersedes
  this ADR's provenance rule: the automatic project-local layer may no longer
  tune `threshold-percent` or `keep-tokens`; it may only disable compaction.
