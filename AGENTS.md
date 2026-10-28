# Agent Instructions

## Project

`cake-repl` is a single-binary Go terminal REPL (Bubble Tea TUI) that drives the
external [`cake`](https://github.com/travisennis/cake) CLI as an engine: each
prompt spawns one `cake --output-format stream-json` process and renders its
NDJSON event stream live. A startup `-resume <uuid>` also uses cake's read-only
`replay <uuid>` stream to hydrate the visible timeline; a bare `-resume` opens a
picker of this directory's sessions instead.

Compatibility surfaces — preserve unless the task explicitly changes them:

- **cake contract**: run cake only in stream-json output mode with the
  documented live session, fork, no-session, model, profile, tools, no-tools,
  add-dir, toolbox, sandbox, no-skills, skills, and system-prompt flags, or the
  supported read-only `replay <uuid>` and `sessions list --json` commands; never
  read cake session files, parse its human text, or import its internals.
- **compaction contract**: the opt-in between-turn compactor is run only through
  `internal/compact` as `cake-compact compact -json -threshold … <session-id>`,
  with exactly one threshold mode: `-threshold-tokens` for the absolute
  `max-context-tokens`/`trigger-tokens` policy or `-threshold-percent` for the
  legacy fallback. The ceiling is never passed as the trigger, and the
  compactor is never run with `-fork`; only its one JSON document is decoded. The
  session id stays pinned; an `in_place:false` or mismatched result is rejected.
  Only trusted config (XDG or an explicit `-config`) may enable or tune it; the
  automatic project-local layer may only disable it. See
  [ADR 023](docs/adr/023-compact-context-between-turns-through-an-external-compactor.md),
  [ADR 024](docs/adr/024-ignore-project-local-compaction-tuning.md), and
  [ADR 025](docs/adr/025-add-an-absolute-context-trigger-with-a-separate-quality-ceiling.md).
- **suggestion contract**: the opt-in after-turn suggester is run only through
  `internal/suggest` as
  `cake-suggest suggest -json [-cake-bin …] [-model …] [-profile …]
  [-reasoning-effort …] -- <session-id>`, and only its one JSON document is
  decoded. It is read-only, never gates input, and its result may populate the
  composer only while the request and session identities still match and the
  composer is empty. The resolved `[suggestions] model` / `profile` values are
  passed through, falling back independently to the turn selection;
  `reasoning-effort` is passed only when set. The request starts alongside an
  automatic compaction; a result that arrives while the compacting gate is
  closed is held and applied after the compactor settles. `Ctrl+S` submits the
  ghost as a prompt and never as a slash command; typing dismisses it. Only
  trusted config may enable it, select the executable, or tune its model or
  effort; the automatic project-local layer may only disable it. See
  [ADR 028](docs/adr/028-offer-after-turn-prompt-suggestions-from-an-external-suggester.md),
  [ADR 030](docs/adr/030-let-prompt-suggestions-select-their-own-model-and-profile.md),
  [ADR 031](docs/adr/031-run-after-turn-suggestions-alongside-compaction.md),
  and
  [ADR 032](docs/adr/032-let-prompt-suggestions-select-reasoning-effort.md).
- **stream-json schema** (`internal/cake/events.go`): decode forward-compatibly.
- **CLI flags and config shape**, mirrored in `README.md`, guardrails, and ADRs.
- **Slash commands and key bindings**, mirrored in `README.md` and `HelpText`.
- **Session run-mode behavior**, which prevents session hijack.
- **Secrets**: raw stream content goes only to the debug log, written
  owner-only at creation (an existing file's permissions are not narrowed).
- **The Go MSRV and the pinned lint, vulnerability, and release tools**;
  `go.mod` and the `justfile` are the authority for the versions.

## Operating Loop

0. Run `ahm prime` before any work to prepare the worktree and get the briefing;
   re-run it after context compaction.
1. Classify the request before editing. If it names a task or an ADR, inspect
   it through `ahm` before choosing implementation work; design plans and other
   project documents are read directly. Direct code, CLI, docs, and repository
   work starts immediately.
2. For a Pending task, run `ahm task start <id>` to begin its lifecycle.
3. Select the route below, load only its docs, and state both before editing.
4. Decide whether the work edits repository files. Read-only work — audits,
   research, recommendations, backlog grooming — stays on the current branch
   with no branch or commit. If edits are needed, create the branch immediately
   before the first edit: `just branch <type>/<slug>`, or a linked worktree
   beside another agent for parallel work. (Release prep is the exception; see
   [Repository Rules](#repository-rules).)
5. Preserve compatibility unless explicitly changed; edit surgically and
   verify according to risk.
6. Before handoff or commit after code changes, run the
   [`preflight`](.agents/skills/preflight/SKILL.md) skill in a subagent. It owns
   the review scale, the lens selection, the finding threshold, the escalation
   trigger, and the fix-and-rerun loop. Consult the
   [documentation impact matrix](docs/guardrails/documentation.md) for
   durable-surface changes.
7. For task-backed work, run `ahm task complete <id>` to close the task
   lifecycle.

Large or cross-cutting work requires a design plan under `docs/exec-plans/`,
written per the [ExecPlan workflow](docs/workflow/exec-plans.md).

Specialized workflow docs override this file when they conflict.

When choosing build, test, lint, verification, or commit-prep commands, read
[`CONTRIBUTING.md`](CONTRIBUTING.md) — it is the canonical command catalog.

## Workflow Routing

### cake Integration And Stream-JSON

Use for `internal/cake/`, `internal/compact/`, `internal/suggest/`, cake CLI
arguments, the external compactor and suggester contracts, or event
consumption.

Consult:

- [cake integration and stream-json](docs/guardrails/cake-integration-and-stream-json.md),
  for the engine, compactor, and suggester contracts and the decoding rules —
  this is the core external contract.
- [`ARCHITECTURE.md`](ARCHITECTURE.md), for engine isolation, replay hydration,
  and the module map.

Decode forward-compatibly; never cross the engine boundary.

### CLI, Slash Commands, And Output

Use for flags, slash commands, key bindings, or `internal/ui/` rendering.

Consult:

- [CLI, commands, and user output](docs/guardrails/cli-and-user-output.md), for
  flag, slash-command, key-binding, and rendering expectations.
- [ADR 003](docs/adr/003-tool-output-expansion-key-binding.md), for the
  tool-output expansion binding.
- [ADR 022](docs/adr/022-generate-osc-8-hyperlinks-for-wrapped-assistant-urls.md),
  for the generated hyperlinks that keep wrapped assistant URLs clickable.
- [`CONTRIBUTING.md`](CONTRIBUTING.md), for the commands that verify the change.

Keep `-no-color` usable.

### Sessions, Security, And Subprocess Lifecycle

Use for the run-mode state machine, subprocess lifecycle, compaction gating and
cancellation, or the debug log.

Consult:

- [Sessions, security, and subprocess lifecycle](docs/guardrails/session-and-security.md),
  for the state machine, cancellation, and what may be written to disk.
- [ADR 001](docs/adr/001-session-run-mode-pinned-to-resume-to-prevent-hijack.md),
  for why run mode pins to resume.
- [ADR 004](docs/adr/004-ctrl-n-creates-an-isolated-new-session-boundary.md), for
  the new-session boundary.
- [ADR 011](docs/adr/011-replay-resumed-sessions-through-cake-stream-json.md), for
  the replay invocation and hydration decision.
- [ADR 027](docs/adr/027-isolate-engine-subprocesses-from-the-repl-controlling-terminal.md),
  for the engine's POSIX session boundary, cancellation group kill, and suspend
  coordination.
- [ADR 028](docs/adr/028-offer-after-turn-prompt-suggestions-from-an-external-suggester.md),
  for the read-only suggester request, its composer application rules, and its
  cancellation paths; [ADR 032](docs/adr/032-let-prompt-suggestions-select-reasoning-effort.md),
  for the suggestion reasoning-effort selection.

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

### Tasks, ADRs, And Design Plans

Use for `ahm` records, task and ADR lifecycle, durable decisions, or design
plans.

Consult:

- [Task workflow](docs/workflow/tasks.md), for choosing, triaging, starting,
  and completing tasks.
- [ADR workflow](docs/workflow/adrs.md), for when a decision needs an ADR and
  how to create, supersede, and index one.
- [ExecPlan workflow](docs/workflow/exec-plans.md), for the design-plan format
  kept under `docs/exec-plans/`.

`ahm` manages only task records and ADR records: ADR records stay under
`docs/adr/`, and task records live in the user-level store described in
[ADR 013](docs/adr/013-store-ahm-task-records-in-the-user-level-home-store.md).
A design plan is an ordinary project file, not an `ahm` record.

### Agent Instructions And Skills

Use for changes to this file, `.agents/skills/`, or any other prose whose
purpose is to change how an agent behaves.

Consult:

- [Agent-facing instructions](docs/guardrails/agent-instructions.md), for the
  evidence a behavior-shaping edit requires.

## Repository Rules

- Work happens on a feature branch: one branch per task, cut from an up-to-date
  `master`, with commits made freely on the branch; hand off the fully
  committed branch for review. Read-only work stays on the current branch.
  Release prep is the exception and commits to `master`. See
  [Commit Workflow](CONTRIBUTING.md#commit-workflow).
- Do not push, merge, or delete a branch unless explicitly asked.
- Assume uncommitted changes may belong to the user (e.g. untracked `review.md`,
  `plan.md`).
- Do not revert, overwrite, or clean files you did not intentionally change.
- Inspect `git status --short` before broad edits.
- Report relevant remaining changes before handoff.

## Handoff

End with the route you took and the documents you loaded, what changed, the
exact checks you ran, remaining risks or skipped checks, and actionable next
steps. For commits, include the branch name and whether it is fully committed,
the commit hashes, the worktree status, and any leftover modified, deleted, or
untracked files.
