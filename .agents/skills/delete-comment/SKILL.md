---
name: delete-comment
description: Audit and remove code comments from a scope in this repository. Use when asked to clean up comments, ban comments, reduce comment noise, or repeat the comment audit behind the comment-free source rule.
---

# Delete Comment

Audit every comment in the chosen scope, remove the ones that do not earn their
place, and leave the durable reasoning in an existing project document. The
endpoint is: Go source and the `justfile` have no prose comments, only machine
directives.

## Scope

Pick one scope and state it before editing:

- the whole Go tree: `.`;
- one package: `internal/app`, `internal/ui`, and so on;
- one file or one section, for a read-only audit.

Read `AGENTS.md` and `CONTRIBUTING.md` before starting. Work on a feature branch.
Do not add tests; this repository has none.

## Inventory

1. List every comment group and its context:

   ```bash
   rg -n '//' --glob '*.go' <scope>
   ```

   Parse with `go/parser` if the scope is large. Include the `justfile` when the
   scope is the whole repository.

2. Record the count before editing:

   ```bash
   just comment-count
   ```

3. List the machine directives that must survive:

   ```bash
   rg -n '//go:|// \+build|//line |//export |//extern |//nolint|// #nosec|//lint:|//revive:|// Code generated' --glob '*.go' <scope>
   ```

   Never delete a directive to satisfy the checker. The checker allows them.

## Classify

Read the code around each comment. Put each group in exactly one category.

| Category | Test | Action |
| --- | --- | --- |
| Tautology | Repeats the identifier, signature, type, field name, or the next line. | Delete. |
| Stale | References removed tests, task numbers, old names, or behavior that changed. | Delete. |
| Excuse | Justifies a workaround, shortcut, platform gap, duplicated path, or "best effort" that should be fixed. | Delete and fix the hack in the same change. |
| Decision Record | Explains an important, non-obvious decision: an invariant, ordering, security boundary, compatibility rule, or tradeoff that a future editor could reasonably undo. | Put the rule in the owning existing document, then delete. |

Examples from the 2026-11-20 audit:

- `// CommandKind identifies a slash command.` — tautology.
- `// It is a field so the platform helper can be stubbed` — stale after the
  test suite was removed.
- `// No line cap: pasting a long multi-line prompt must not freeze Enter` —
  excuse; the composer needed a bound, not a comment.
- The suspend cycle ordering and sanitizer allowlist — decision records; they
  belong in `ARCHITECTURE.md` or the relevant guardrail, not in a comment.

Do not keep a comment because it is interesting. If it is not important enough
to state as a rule in an existing document, it is not important enough to stay
in the source.

## Act

### Tautology and stale

Delete the comment. Re-read the surrounding declaration; if the name no longer
explains the behavior, improve the name instead of restoring the comment.

### Excuse

Delete the comment and fix the underlying hack now. This repository does not use
GitHub issues as a deferred-work backlog. If the fix is genuinely outside the
chosen scope, stop and ask instead of writing a comment or filing an issue.

### Decision record

Do not create an ADR or a separate decision log. This repository deliberately
has none. State the durable rule in the place a future editor already reads:

- `ARCHITECTURE.md` for module boundaries, dependency direction, and invariants;
- `docs/guardrails/` for workflow, security, CLI, and documentation rules;
- `README.md` for user-visible behavior;
- the code itself for a rule the names and structure can carry.

Add only the missing sentence to the owning document. Do not copy the comment.
Then delete the comment.

## Ratchet

The checker supports a partial migration. Do not try to finish the whole tree
in one pass if the scope is large.

1. Set `tools/commentcheck/limit.txt` to the current count from
   `just comment-count`. This is the one deliberate allowance for the migration.
2. Run `just comment-baseline` to record the comments that remain.
3. Delete what you classified. After each pass, lower the limit to the new
   count and run `just comment-baseline` again.
4. Never raise the limit. A change that raises it is not a cleanup.
5. Before handoff, run `just comment-check`. It fails on a new comment, an extra
   duplicate, or a stale allowlist entry. If it fails because the baseline is
   stale, lower the limit and regenerate the baseline.

When the scope reaches zero, the allowlist is empty and the limit is `0`.

## Verify

Run:

```bash
just fmt-check
just vet
just lint
just deadcode
just build
```

`just lint` runs `comment-check` first. For a broad change, run `just ci`. Read
the changed documents back before handoff.

## Report

Report:

- the scope and the count before and after (`just comment-count`);
- lines removed, measured from `git diff --shortstat` or
  `git diff -U0 -- '*.go' | grep -cE '^-\s*//'`;
- counts per category, with a few concrete examples;
- the existing documents changed for decision records;
- the exact verification commands and any skipped check;
- the branch name, commit hashes, and worktree status.
