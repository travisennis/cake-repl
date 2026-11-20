# Contributing

This is the contributor reference for `cake-repl`: setup, the command catalog,
code style, verification expectations, and the branch/commit/release workflow.
For the high-level map see [`ARCHITECTURE.md`](ARCHITECTURE.md); for agent
routing see [`AGENTS.md`](AGENTS.md).

## Requirements

- Go `1.27.3` or newer (the module targets `go 1.27.3`).
- [`just`](https://github.com/casey/just) for the task runner (optional; the
  underlying `go` commands work too).
- A `cake` binary on `PATH` only to use the app. Nothing in the build or lint
  gate needs one.

Install the pinned lint/vuln/release tools once:

```bash
just install-tools   # golangci-lint, govulncheck, deadcode, goreleaser (versions pinned in justfile)
```

## No tests

This repository has no test suite and does not accept new tests. Do not create
`_test.go` files, testdata, benchmarks, or test-only helpers, and do not run
`go test` or any test harness. If a change seems to need a test, verify the
behavior by running the REPL against a real `cake` binary and describe that
verification in the handoff.

## Command catalog

Prefer `justfile` targets. The `just ci` gate is the contract for "broadly
verified" before handoff; a `just verify` alias is also available.

```bash
just build          # go build -trimpath -o bin/cake-repl ./cmd/cake-repl
just run *args      # go run ./cmd/cake-repl
just install        # go install -trimpath ./cmd/cake-repl
just branch <type>/<slug>
                    # create a task branch from up-to-date master
just integrate <type>/<slug>
                    # rebase a branch onto master and fast-forward
just quick          # go vet ./...
just fmt            # go fmt ./...
just fmt-check      # fail if gofmt would change files
just tidy           # go mod tidy
just tidy-check     # fail if go mod tidy would change files
just update-deps    # go get -u ./... && go mod tidy
just vet            # go vet ./...
just lint           # golangci-lint run
just deadcode       # fail on unreachable functions (whole-program reachability)
just vuln           # govulncheck ./...
just release-check  # goreleaser check + snapshot build
just fix            # tidy + fmt
just ci             # full gate: fmt-check tidy-check vet lint deadcode vuln build release-check
just verify         # alias for ci
```

Without `just`, run the underlying `go` commands:

```bash
# Build everything (fast compile check)
go build ./...
# Build just the binary
go build ./cmd/cake-repl
# Format
go fmt ./...
# Tidy
go mod tidy
# Vet
go vet ./...
```

## Code style

- Standard Go style: `gofmt` tabs, short package-local names, explicit error
  handling. `golangci-lint` config lives in `.golangci.yml`.
- Respect package boundaries (see [`ARCHITECTURE.md`](ARCHITECTURE.md)): app
  behavior in `internal/app`, cake process/event concerns in `internal/cake`,
  terminal rendering in `internal/ui`. The dependency direction is one-way.
- Avoid speculative abstractions; keep `internal/ui` side-effect-free.
- Keep command and flag names aligned with `README.md`, `HelpText`, and the
  cake CLI contract.

## Verification expectations

- Focused change: run `just build` and `just vet`; run `just lint` when the
  change touches logic the linter would see.
- Broader change before handoff: run `just ci`.
- There is no test or benchmark step in the gate, and none should be added.

## Project-specific command pitfalls

- **Preserve the cake engine contract.** Live prompts run only as
  `--output-format stream-json` with `--resume` (or initial `--fork`),
  `--no-session`, `--model`/`--profile`/`--tools`/`--no-tools`,
  `--add-dir`/`--toolbox`/`--sandbox`, `--no-skills`/`--skills`,
  `--system-prompt`, and `--` before the prompt. Startup `-resume` may
  additionally run the read-only `cake --output-format stream-json replay
  <uuid>` command. Never read cake session files, parse its human text, or
  import cake internals. The REPL drives cake purely through the NDJSON
  stream.
- **`just ci` is the final pre-handoff gate.** For code, config, or dependency
  changes, run `just ci` before calling the work done. For doc-only changes,
  skip it and explain the skip.
- **`just lint` cannot see exported dead code.** `unused` reports unexported
  symbols only, and every package here lives under `internal/`, so an exported
  helper nothing calls passes the gate. `just deadcode` closes that gap using
  whole-program reachability from `main`, and it is part of `just ci`. An
  exported API that is ahead of its caller is a finding, not an exception:
  land the caller or do not land the API.

## Commit Workflow

Work happens on a feature branch cut from an up-to-date `master`. One branch
holds one task; read-only work (audits, research, backlog grooming) stays on the
current branch. Maintainer work does not use pull requests: the maintainer
integrates the branch into `master` locally and pushes `master`. Pull requests
still exist for Dependabot and outside contributors. Release prep is the
exception and commits to `master` (see [Release](#release)).

The standard sequence:

1. Create the branch: `just branch <type>/<slug>`, which syncs `master` first.
   Use a linked worktree instead when two streams of work run in parallel.
2. Implement, committing freely on the branch.
3. Do not push, merge, or delete the branch unless explicitly asked. Hand off
   the fully committed branch; merging into `master`, pushing `master`, and
   branch cleanup happen only with explicit permission (see
   [Integrating A Branch](#integrating-a-branch)).
4. Hand off with the branch name and whether it is fully committed, the commit
   hashes, the worktree status, and any remaining modified, deleted, or
   untracked files.

Commit messages must use Conventional Commits:

```text
<type>[(scope)]: <description>
```

Types: `feat`, `fix`, `docs`, `style`, `refactor`, `perf`, `build`, `ci`,
`chore`, `revert`. A local `pre-commit` config
(`.pre-commit-config.yaml`) runs `fmt-check`, `tidy-check`, and `lint`, and
`conventional-pre-commit` checks the commit message. Install both hooks in a
clone with `just hooks` (requires [`prek`](https://github.com/j178/prek)).

```text
feat: add session status command
fix: handle malformed stream-json lines
docs: update install instructions
```

For multiline commit messages, use a heredoc — not command substitution inside
`git commit -m`:

```bash
git commit -F - <<'EOF'
feat: short summary

Body paragraph with detail.
EOF
```

## Integrating A Branch

Integration is local; maintainer work does not go through a pull request. From
a clean `master`, `just integrate <type>/<slug>` rebases the branch onto
`master` and fast-forwards, so `master` stays linear with no merge commits:

```bash
git switch master
just integrate feat/timeline-compaction
just ci
git push origin master                    # push only with explicit permission
git branch -d feat/timeline-compaction    # delete only with explicit permission
```

Integrate one branch at a time. Each run rebases its branch onto the current
`master`, so overlapping work surfaces as a conflict to resolve in that branch
rather than a merge to untangle.

The recipe preserves the branch's commits. To land a task as a single commit,
tidy the branch first with `git switch <branch> && git rebase -i master`,
squashing the work-in-progress commits, then run `just integrate`. Prefer this
for agent branches, which commit freely while working.

The recipe refuses to run unless you are on `master`, the worktree is clean,
the branch exists, and the branch still has commits to integrate. It syncs
`master` with `git pull --ff-only` before rebasing. A rebase conflict stops it
mid-rebase on a detached HEAD: resolve each conflict, `git add` the files, run
`git rebase --continue` (which returns you to the branch), then switch back to
`master` and rerun `just integrate`. `git rebase --abort` backs the rebase out.

Pushing `master` and deleting the branch are explicit-permission actions.

## Release

- Release prep is the one exception to branch-first: release commits land on
  `master` directly, and the `v*` tag is created on `master`.
- Releases are cut by pushing a `v*` tag; the `Release` workflow runs `just ci`
  then `goreleaser release --clean`.
- Validate release config locally with `just release-check` before tagging.
- Tool versions (golangci-lint, govulncheck, deadcode, goreleaser) are pinned in
  the `justfile`; bump them deliberately and re-run `just ci`. See
  [`docs/guardrails/dependencies-build-ci-release.md`](docs/guardrails/dependencies-build-ci-release.md).
