# Agent Instructions

## Project

`cake-repl` is a single-binary Go terminal REPL (Bubble Tea TUI) that drives the
external [`cake`](https://github.com/travisennis/cake) CLI as an engine: each
prompt spawns one `cake --output-format stream-json` process and renders its
NDJSON event stream live.

Compatibility surfaces — preserve unless the task explicitly changes them:

- **cake contract**: run cake only as `--output-format stream-json` with
  `--continue`/`--resume`/`--model`/`--profile`; never read cake session files,
  parse its human text, or import its internals.
- **stream-json schema** (`internal/cake/events.go`): decode forward-compatibly.
- **CLI flags, slash commands, key bindings** (mirrored in `README.md` + `HelpText`).
- **Session run-mode behavior** pins to `--resume` to prevent hijack.
- **Secrets**: raw stream content goes only to `--debug-log` (`0o600`).
- **Go MSRV** `1.26.3`+ and the pinned lint/vuln/release tools.

## Operating Loop

1. Classify the request before editing.
2. Load only the routed docs needed for that request.
3. Preserve compatibility surfaces unless explicitly changed.
4. Keep edits surgical and verify according to risk (see the testing route).
5. Hand off with changes, checks, and remaining risk.

When this file conflicts with a specialized doc for that workflow, the
specialized doc wins.

## Workflow Routing

### cake Integration And Stream-JSON

Use for changes to `internal/cake/` (events, parser, runner), cake CLI args, or
how `app` consumes events. Consult
[`docs/guardrails/cake-integration-and-stream-json.md`](docs/guardrails/cake-integration-and-stream-json.md)
and [`ARCHITECTURE.md`](ARCHITECTURE.md). Decode forward-compatibly; never cross the engine-isolation boundary.

### CLI, Slash Commands, And Output

Use for flags, slash commands, key bindings, or `internal/ui/` rendering.
Consult [`docs/guardrails/cli-and-user-output.md`](docs/guardrails/cli-and-user-output.md).
Update `README.md`, `HelpText`, and this file together; keep `--no-color` usable.

### Sessions, Security, And Subprocess Lifecycle

Use for the run-mode state machine, subprocess start/cancel, or `--debug-log`.
Consult [`docs/guardrails/session-and-security.md`](docs/guardrails/session-and-security.md).
Preserve session-hijack prevention and never leak raw stream content.

### Core Runtime, UI, And Implementation Quality

Use for `internal/app` state/logic, `internal/ui` rendering, and code style.
Consult [`ARCHITECTURE.md`](ARCHITECTURE.md) (boundaries) and [`CONTRIBUTING.md`](CONTRIBUTING.md) (style).
Keep `internal/ui` side-effect-free and the one-way dependency direction intact.

### Tests And Verification

Use when adding tests or deciding what to run. Consult
[`docs/guardrails/testing-and-verification.md`](docs/guardrails/testing-and-verification.md).
Runner tests must not require a real `cake`; run `just test-race` for the runner, `just ci` before handoff.

### Dependencies, Build, CI, And Release

Use for `go.mod`, the `justfile`, workflows, `.goreleaser.yaml`, or linters.
Consult [`docs/guardrails/dependencies-build-ci-release.md`](docs/guardrails/dependencies-build-ci-release.md)
and [`CONTRIBUTING.md`](CONTRIBUTING.md). Keep pinned tool versions and the Go
MSRV consistent across `go.mod`, docs, and CI.

### Documentation

For doc work, read `.agents/DOCS.md` and `docs/guardrails/documentation.md` first. Also use them when behavior, config, architecture, workflow, or compatibility changes require doc updates.

Docs-only changes: keep `README.md`, `HelpText`, and routing in sync, and move detailed rules into the right guardrail rather than growing this file.

### Workflow Overlays
These overlays do not replace the specific workflow routes above. Use them first
to identify or manage the work item, then re-classify the concrete task and load
the relevant routed workflow docs before editing.

When asked to create, choose, update, or work on a task, read `.agents/TASKS.md`,
inspect the task with `ahm task ...`, open the task file, then return to
Workflow Routing and choose the specific route or routes required by the task
content. When a task, workflow doc, or user request calls for an ExecPlan, read
`.agents/PLANS.md`. When one calls for an ADR, read [docs/adr/README.md](docs/adr/README.md).
When asked to create, update, organize, or use research, read `.agents/RESEARCH.md`,
then use `.agents/.research/index.md` as the map.

## Repository Rules

- Do not commit or push unless explicitly asked.
- Assume uncommitted changes may belong to the user (e.g. untracked `review.md`,
  `plan.md`).
- Do not revert, overwrite, or clean files you did not intentionally change.
- Inspect `git status --short` before broad edits.
- Report relevant remaining changes before handoff.

## Handoff

End with what changed, the exact checks you ran, remaining risks or skipped
checks, and actionable next steps. For commits, include the hash, worktree
cleanliness, and any leftover changes.
