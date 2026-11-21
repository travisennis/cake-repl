---
status: accepted
date: 2026-11-20
---
# Keep the justfile recipes safe and tool-pinned

## Context and Problem Statement

The justfile is executable source. Several recipes exist because `just` and the
Go toolchain have non-obvious behavior that a future edit could undo: argument
interpolation, branch-name injection, local-master rebasing, and tools that exit
zero even when they find a problem.

## Decision

- `set positional-arguments` is global. `just` interpolates a variadic argument
  list by joining it with spaces, which discards quoting; recipes that forward
  arguments read `"$@"`. The only forwarding recipe is `run`.
- `_check-branch-name` rejects characters that the shell would interpret before
  Git sees the ref. `just` interpolates recipe arguments into shell source, so a
  branch name such as `feat/x$(...)` would execute. The accepted set is letters,
  digits, dot, underscore, hyphen, and `/`, with an optional commit-type prefix.
- `tool_dir` resolves `GOBIN`, then `GOPATH/bin`, and puts it first on `PATH`.
  Recipes prefer the pins from `install-tools` over whatever is installed
  globally.
- `branch` switches to local `master`, pulls `--ff-only`, and creates a
  no-track branch. Local master is the base because it may carry unpushed
  commits that cutting from `origin/master` would drop.
- `integrate` rebases the task branch onto master and fast-forwards. It refuses
  a dirty worktree, a branch with no commits, or a run from anywhere but
  `master`. A conflict stops mid-rebase with instructions to resolve, add, and
  continue.
- `deadcode` runs the tool and turns its output into a non-zero exit. The tool
  itself always exits zero.
- `install-tools-ci` uses prebuilt binaries where they exist; goreleaser comes
  from the workflow action; govulncheck and deadcode are built from source
  because they publish no binaries.

## Consequences

- Good: a recipe cannot be redirected into shell execution by a crafted branch
  name.
- Good: local and CI used the same pinned tool versions.
- Good: a task branch stays linear and one task per branch.
- Bad: the justfile has no comments; these reasons live here.

## More Information

- [`justfile`](../../justfile)
- [`CONTRIBUTING.md`](../../CONTRIBUTING.md)
