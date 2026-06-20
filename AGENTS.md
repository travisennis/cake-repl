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
- **Secrets**: raw stream content goes only to `-debug-log` (`0o600`).
- **Go MSRV** `1.26.3`+ and the pinned lint/vuln/release tools.

## Operating Loop

1. Do managed-work intake first.
   - If the request is about a task, ExecPlan, ADR, or research note, use `ahm`
     to understand that managed work item before choosing implementation docs.
   - If the request is directly about code, CLI behavior, tests, docs, build,
     release, or repo mechanics, skip `ahm` intake and classify directly.
2. Classify the request — choose the workflow route below.
3. Load only the routed docs needed for that request.
4. State the selected route and loaded docs in handoff.
5. Preserve compatibility surfaces unless explicitly changed.
6. Keep edits surgical and verify according to risk (see the testing route).
7. Hand off with changes, exact checks, and remaining risk.

When choosing build, test, lint, verification, or commit-prep commands, read
[`CONTRIBUTING.md`](CONTRIBUTING.md) — it is the canonical command catalog.

When this file conflicts with a specialized doc for that workflow, the
specialized doc wins.

## Managed Work Intake With `ahm`

`ahm` is for understanding and managing higher-order workflow records. It is
not the implementation route. Use it first when the user asks about a managed
work item, then return to Workflow Routing and choose the route for the actual
change.

Use these entry points:

- Tasks: run `ahm context task`, inspect the relevant task with `ahm task ...`,
  and open the task file before editing.
- ExecPlans: run `ahm context plan` when the request or task calls for an
  ExecPlan.
- ADRs: run `ahm context adr` when the request or task calls for an ADR.
- Research: run `ahm context research` and use `.agents/.research/index.md` as
  the map when asked to create, update, organize, or use research.
- General session briefing: run `ahm context` only when asked for broad project
  context or when no narrower managed-work context applies.

After `ahm` intake, re-classify the discovered work under Workflow Routing.
For example, a task about CLI flags still uses the CLI routing docs; a task
about atomic writes still uses the Safety routing docs; a task about templates
or workflow formats still uses the Workflow State routing docs.


## Workflow Routing

### cake Integration And Stream-JSON

Use for changes to `internal/cake/` (events, parser, runner), cake CLI args, or
how `app` consumes events. Consult
[`docs/guardrails/cake-integration-and-stream-json.md`](docs/guardrails/cake-integration-and-stream-json.md)
and [`ARCHITECTURE.md`](ARCHITECTURE.md). Decode forward-compatibly; never cross the engine-isolation boundary.

### CLI, Slash Commands, And Output

Use for flags, slash commands, key bindings, or `internal/ui/` rendering.
Consult [`docs/guardrails/cli-and-user-output.md`](docs/guardrails/cli-and-user-output.md)
and [`CONTRIBUTING.md`](CONTRIBUTING.md) for verification commands.
Update `README.md`, `HelpText`, and this file together; keep `-no-color` usable.

### Sessions, Security, And Subprocess Lifecycle

Use for the run-mode state machine, subprocess start/cancel, or `-debug-log`.
Consult [`docs/guardrails/session-and-security.md`](docs/guardrails/session-and-security.md).
Preserve session-hijack prevention and never leak raw stream content.

### Core Runtime, UI, And Implementation Quality

Use for `internal/app` state/logic, `internal/ui` rendering, and code style.
Consult [`ARCHITECTURE.md`](ARCHITECTURE.md) (boundaries) and
[`CONTRIBUTING.md`](CONTRIBUTING.md) for code style, verification, and local
commands.
Keep `internal/ui` side-effect-free and the one-way dependency direction intact.

### Tests And Verification

Use when adding tests or deciding what to run. Consult
[`docs/guardrails/testing-and-verification.md`](docs/guardrails/testing-and-verification.md)
and [`CONTRIBUTING.md`](CONTRIBUTING.md) for the command catalog.
Runner tests must not require a real `cake`; run `just test-race` for the runner, `just ci` before handoff.

### Dependencies, Build, CI, And Release

Use for `go.mod`, the `justfile`, workflows, `.goreleaser.yaml`, or linters.
Consult [`docs/guardrails/dependencies-build-ci-release.md`](docs/guardrails/dependencies-build-ci-release.md)
and [`CONTRIBUTING.md`](CONTRIBUTING.md) for setup, verification, commands, PR
workflow, and commit conventions. Keep pinned tool versions and the Go MSRV
consistent across `go.mod`, docs, and CI.

### Documentation

For doc work, consult `docs/guardrails/documentation.md` first. Also use it when behavior, config, architecture, workflow, or compatibility changes require doc updates.

Docs-only changes: keep `README.md`, `HelpText`, and routing in sync, and move detailed rules into the right guardrail rather than growing this file.

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
