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
- **CLI flags and config shape** (mirrored in `README.md`, guardrails, and
  ADRs where applicable).
- **Slash commands and key bindings** (mirrored in `README.md` and
  `HelpText`).
- **Session run-mode behavior** pins to `--resume` to prevent hijack.
- **Secrets**: raw stream content goes only to `-debug-log` (`0o600`).
- **Go MSRV** `1.26.3`+ and the pinned lint/vuln/release tools.

## Operating Loop

0. Run `ahm prime` before any work to prepare the worktree and get the briefing;
   re-run it after context compaction.
1. Use `ahm` intake first for tasks, ExecPlans, ADRs, or research; classify
   direct code, CLI, docs, or repository work immediately.
2. For a Pending task, run `ahm task start <id>` to begin its lifecycle.
3. Select the route below, load only its docs, and state both before editing.
4. Preserve compatibility unless explicitly changed; edit surgically and
   verify according to risk.
5. After implementation edits, run a review in a subagent, fix all findings, and rerun
   until clean; reconsider approaches that do not converge.
6. Consult a deeper reasoning model via a subagent for unclear design, debugging, or path
   choices when stuck.
7. Before handoff or commit after code changes, run the
   [`preflight`](.agents/skills/preflight/SKILL.md) skill in a subagent and consult the
   [documentation impact matrix](docs/guardrails/documentation.md) for
   durable-surface changes.
8. For task-backed work, run `ahm task complete <id>` to close the task lifecycle.

Specialized workflow docs override this file when they conflict.

When choosing build, test, lint, verification, or commit-prep commands, read
[`CONTRIBUTING.md`](CONTRIBUTING.md) — it is the canonical command catalog.

## Workflow Routing

### cake Integration And Stream-JSON

Use for `internal/cake/`, cake CLI args, or event consumption. Consult
[`docs/guardrails/cake-integration-and-stream-json.md`](docs/guardrails/cake-integration-and-stream-json.md)
and [`ARCHITECTURE.md`](ARCHITECTURE.md). Decode forward-compatibly; never cross the engine boundary.

### CLI, Slash Commands, And Output

Use for flags, slash commands, key bindings, or `internal/ui/` rendering.
Consult [`docs/guardrails/cli-and-user-output.md`](docs/guardrails/cli-and-user-output.md)
and [`CONTRIBUTING.md`](CONTRIBUTING.md). Keep `-no-color` usable.

### Sessions, Security, And Subprocess Lifecycle

Use for run-mode state machine, subprocess lifecycle, or `-debug-log`.
Consult [`docs/guardrails/session-and-security.md`](docs/guardrails/session-and-security.md).
Preserve session-hijack prevention and never leak raw stream content.

### Core Runtime, UI, And Implementation Quality

Use for `internal/app` logic, `internal/ui` rendering, and code style.
Consult [`ARCHITECTURE.md`](ARCHITECTURE.md) and [`CONTRIBUTING.md`](CONTRIBUTING.md).
Keep `internal/ui` side-effect-free and the one-way dependency direction intact.

### Tests And Verification

Use for adding tests or deciding what to run. Consult
[`docs/guardrails/testing-and-verification.md`](docs/guardrails/testing-and-verification.md)
and [`CONTRIBUTING.md`](CONTRIBUTING.md). Runner tests must not require a real `cake`;
run `just test-race` for the runner, `just ci` before handoff.

### Dependencies, Build, CI, And Release

Use for `go.mod`, `justfile`, workflows, `.goreleaser.yaml`, or linters.
Consult [`docs/guardrails/dependencies-build-ci-release.md`](docs/guardrails/dependencies-build-ci-release.md)
and [`CONTRIBUTING.md`](CONTRIBUTING.md). Keep pinned tool versions and the Go MSRV
consistent across `go.mod`, docs, and CI.

### Documentation

Use for doc work or when behavior, config, architecture, or compatibility changes
require doc updates. Consult `docs/guardrails/documentation.md` first. Keep
`README.md`, `HelpText`, guardrails, ADRs, and routing in sync; move detailed
rules into the right guardrail rather than growing this file.

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
