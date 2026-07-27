# Agent Instructions

## Project

`cake-repl` is a single-binary Go terminal REPL (Bubble Tea TUI) that drives the
external [`cake`](https://github.com/travisennis/cake) CLI as an engine: each
prompt spawns one `cake --output-format stream-json` process and renders its
NDJSON event stream live.

Compatibility surfaces — preserve unless the task explicitly changes them:

- **cake contract**: run cake only in stream-json output mode with the
  documented session, model, and profile flags; never read cake session files,
  parse its human text, or import its internals.
- **stream-json schema** (`internal/cake/events.go`): decode forward-compatibly.
- **CLI flags and config shape**, mirrored in `README.md`, guardrails, and ADRs.
- **Slash commands and key bindings**, mirrored in `README.md` and `HelpText`.
- **Session run-mode behavior**, which prevents session hijack.
- **Secrets**: raw stream content goes only to the debug log, written
  owner-only.
- **The Go MSRV and the pinned lint, vulnerability, and release tools**;
  `go.mod` and the `justfile` are the authority for the versions.

## Operating Loop

0. Run `ahm prime` before any work to prepare the worktree and get the briefing;
   re-run it after context compaction.
1. Use `ahm` intake first for tasks, ExecPlans, ADRs, or research; classify
   direct code, CLI, docs, or repository work immediately.
2. For a Pending task, run `ahm task start <id>` to begin its lifecycle.
3. Select the route below, load only its docs, and state both before editing.
4. Preserve compatibility unless explicitly changed; edit surgically and
   verify according to risk.
5. Before handoff or commit after code changes, run the
   [`preflight`](.agents/skills/preflight/SKILL.md) skill in a subagent. It owns
   the review scale and the fix-and-rerun loop. If a third round reports
   findings of the same class, stop patching: report the finding class and the
   suspected design flaw, and escalate to a design decision. Consult the
   [documentation impact matrix](docs/guardrails/documentation.md) for
   durable-surface changes.
6. For task-backed work, run `ahm task complete <id>` to close the task
   lifecycle.

Specialized workflow docs override this file when they conflict.

When choosing build, test, lint, verification, or commit-prep commands, read
[`CONTRIBUTING.md`](CONTRIBUTING.md) — it is the canonical command catalog.

## Workflow Routing

### cake Integration And Stream-JSON

Use for `internal/cake/`, cake CLI arguments, or event consumption.

Consult:

- [cake integration and stream-json](docs/guardrails/cake-integration-and-stream-json.md),
  for the engine contract and the decoding rules — this is the core external
  contract.
- [`ARCHITECTURE.md`](ARCHITECTURE.md), for engine isolation and the module map.

Decode forward-compatibly; never cross the engine boundary.

### CLI, Slash Commands, And Output

Use for flags, slash commands, key bindings, or `internal/ui/` rendering.

Consult:

- [CLI, commands, and user output](docs/guardrails/cli-and-user-output.md), for
  flag, slash-command, key-binding, and rendering expectations.
- [ADR 003](docs/adr/003-tool-output-expansion-key-binding.md), for the
  tool-output expansion binding.
- [`CONTRIBUTING.md`](CONTRIBUTING.md), for the commands that verify the change.

Keep `-no-color` usable.

### Sessions, Security, And Subprocess Lifecycle

Use for the run-mode state machine, subprocess lifecycle, or the debug log.

Consult:

- [Sessions, security, and subprocess lifecycle](docs/guardrails/session-and-security.md),
  for the state machine, cancellation, and what may be written to disk.
- [ADR 001](docs/adr/001-session-run-mode-pinned-to-resume-to-prevent-hijack.md),
  for why run mode pins to resume.
- [ADR 004](docs/adr/004-ctrl-n-creates-an-isolated-new-session-boundary.md), for
  the new-session boundary.

Preserve session-hijack prevention and never leak raw stream content.

### Core Runtime, UI, And Implementation Quality

Use for `internal/app` logic, `internal/ui` rendering, and code style.

Consult:

- [`ARCHITECTURE.md`](ARCHITECTURE.md), for the module map, the one-way
  dependency direction, and the architectural invariants.
- [`CONTRIBUTING.md`](CONTRIBUTING.md), for code style and verification
  expectations.

Keep `internal/ui` side-effect-free and the one-way dependency direction intact.

### Tests And Verification

Use for adding tests or deciding what to run.

Consult:

- [Testing and verification](docs/guardrails/testing-and-verification.md), for
  test conventions and the verification ladder.
- [`CONTRIBUTING.md`](CONTRIBUTING.md), for the command definitions themselves.

Runner tests must not require a real `cake`; run `just test-race` for the
runner, `just ci` before handoff.

### Dependencies, Build, CI, And Release

Use for `go.mod`, the `justfile`, workflows, `.goreleaser.yaml`, or linters.

Consult:

- [Dependencies, build, CI, and release](docs/guardrails/dependencies-build-ci-release.md),
  for dependency, tool-pinning, and release policy.
- [`CONTRIBUTING.md`](CONTRIBUTING.md), for the build and release commands.

Keep pinned tool versions and the Go MSRV consistent across `go.mod`, docs, and
CI.

### Documentation

Use for doc work, or when behavior, config, architecture, or compatibility
changes require doc updates.

Consult:

- [Documentation](docs/guardrails/documentation.md), for the impact matrix that
  says which surfaces require which doc updates.

Keep `README.md`, `HelpText`, guardrails, ADRs, and routing in sync; move
detailed rules into the right guardrail rather than growing this file.

### Agent Instructions And Skills

Use for changes to this file, `.agents/skills/`, or any other prose whose
purpose is to change how an agent behaves.

Consult:

- [Agent-facing instructions](docs/guardrails/agent-instructions.md), for the
  evidence a behavior-shaping edit requires.

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
