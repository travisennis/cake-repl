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
  supported read-only `replay <uuid>`, `sessions list --json`, and
  `trust --status --json` commands; never read cake session files, parse its
  human text, or import its internals. The startup trust probe is advisory: it
  decodes only cake's versioned diagnostic document, never writes a trust
  record, and is silent on any failure.
- **compaction contract**: the opt-in between-turn compactor is run only through
  `internal/compact` as `cake-compact compact -json -threshold … <session-id>`,
  with exactly one threshold mode: `-threshold-tokens` for the absolute
  `max-context-tokens`/`trigger-tokens` policy or `-threshold-percent` for the
  legacy fallback. The ceiling is never passed as the trigger, and the
  compactor is never run with `-fork`; only its one JSON document is decoded. The
  session id stays pinned; an `in_place:false` or mismatched result is rejected.
  Only trusted config (XDG or an explicit `-config`) may enable or tune it; the
  automatic project-local layer may only disable it.
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
  closed is held and applied after the compactor settles. While the request is
  in flight, the status line appends `suggesting <n>s` beside the operational
  state, refreshed from the spinner tick the REPL already runs; the composer
  label, hint, and input gate never change for it. `Ctrl+S` inserts the
  ghost into the composer and never submits it; a second `Ctrl+S` submits the
  filled text, which bypasses slash-command parsing only while the text remains
  in the composer unedited. Typing and history recall hide the ghost and
  clearing the composer restores it; completion, submit, retarget, and quit
  discard it. A clean startup `-resume <uuid>` hydration ending at a completed
  turn starts one suggestion. Only trusted config may enable it, select the
  executable, or tune its model or effort; the automatic project-local layer
  may only disable it.
- **stream-json schema** (`internal/cake/events.go`): decode forward-compatibly.
- **CLI flags and config shape**, mirrored in `README.md` and the guardrails.
- **Slash commands and key bindings**, mirrored in `README.md` and `HelpText`.
  `/model` and `/profile` are idle-only, apply to the next prompt, and use
  `default` to clear.
- **Session run-mode behavior**, which prevents session hijack.
- **Secrets**: raw stream content goes only to the debug log, written
  owner-only at creation (an existing file's permissions are not narrowed).
- **The Go MSRV and the pinned lint, vulnerability, and release tools**;
  `go.mod` and the `justfile` are the authority for the versions.

## Operating Loop

1. Classify the request before editing. Direct code, CLI, docs, and repository
   work starts immediately.
2. Select the route below, load only its docs, and state both before editing.
3. Decide whether the work edits repository files. Read-only work — audits,
   research, recommendations, backlog grooming — stays on the current branch
   with no branch or commit. If edits are needed, create the branch immediately
   before the first edit: `just branch <type>/<slug>`, or a linked worktree
   beside another agent for parallel work. (Release prep is the exception and
   commits to `master`.)
4. Preserve compatibility unless explicitly changed; edit surgically, then
   build, format, and lint according to risk.
5. There is no test suite. Do not add one.

Specialized workflow docs override this file when they conflict.

When choosing build, lint, verification, or commit-prep commands, read
[`CONTRIBUTING.md`](CONTRIBUTING.md) — it is the canonical command catalog.

## Verification: no tests

This repository does not write, keep, or run tests. Do not create `_test.go`
files, test fixtures, testdata, benchmarks, or test-only helpers. Do not run
`go test`, coverage, or any test harness. If you encounter a test while working
in a file, delete it rather than updating it.

Verify a change with:

- `just fmt-check`
- `just vet`
- `just lint`
- `just deadcode`
- `just build`

Report the exact commands you ran and anything you could not run, and why.

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
- [`CONTRIBUTING.md`](CONTRIBUTING.md), for the commands that verify the change.

Keep `-no-color` usable.

### Sessions, Security, And Subprocess Lifecycle

Use for the run-mode state machine, subprocess lifecycle, compaction gating and
cancellation, or the debug log.

Consult:

- [Sessions, security, and subprocess lifecycle](docs/guardrails/session-and-security.md),
  for the state machine, cancellation, and what may be written to disk.

Preserve session-hijack prevention and never leak raw stream content.

### Core Runtime, UI, And Implementation Quality

Use for `internal/app` logic, `internal/ui` rendering, and code style.

Consult:

- [`ARCHITECTURE.md`](ARCHITECTURE.md), for the module map, the one-way
  dependency direction, and the architectural invariants.
- [`CONTRIBUTING.md`](CONTRIBUTING.md), for code style.

Keep `internal/ui` side-effect-free and the one-way dependency direction intact.
Keep Go source and the `justfile` comment-free; only machine directives remain.
Record a durable decision in the code, `README.md`, `ARCHITECTURE.md`, or the
owning guardrail where it belongs. Do not create ADRs or separate decision
records. Do not file a GitHub issue for a workaround; fix it in the same change.

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

Keep `README.md`, `HelpText`, guardrails, and routing in sync; move detailed
rules into the right guardrail rather than growing this file.

### Agent Instructions And Skills

Use for changes to this file, `.agents/skills/`, or any other prose whose
purpose is to change how an agent behaves.

Consult:

- [Agent-facing instructions](docs/guardrails/agent-instructions.md), for the
  evidence a behavior-shaping edit requires.

## Repository Rules

- Do not create ADRs, decision logs, or other separate decision records. State
  durable decisions in the code, `README.md`, `ARCHITECTURE.md`, or the owning
  guardrail. Do not use GitHub issues as a backlog: fix the work now, or ask
  before deferring it.
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
- Future work and unresolved questions belong in the response or the issue
  tracker, not in new durable records.

## Handoff

End with the route you took and the documents you loaded, what changed, the
exact checks you ran, remaining risks or skipped checks, and actionable next
steps. For commits, include the branch name and whether it is fully committed,
the commit hashes, the worktree status, and any leftover modified, deleted, or
untracked files.
