---
name: delete-comment
description: Audit and remove code comments from a scope in this repository. Use when asked to clean up comments, ban comments, reduce comment noise, or repeat the comment audit that produced docs/adr/040.
---

# Delete Comment

Audit every comment in the chosen scope, remove the ones that do not earn their
place, and leave the durable reasoning in an ADR. The endpoint is
[ADR 040](../../../docs/adr/040-keep-go-source-comment-free.md): Go source has no
prose comments, only machine directives.

## Scope

Pick one scope and state it before editing:

- the whole Go tree: `.`;
- one package: `internal/app`, `internal/ui`, and so on;
- one file or one section, for a read-only audit.

Read `AGENTS.md`, `CONTRIBUTING.md`, and ADR 040 before starting. Work on a
feature branch. Do not add tests; this repository has none.

## Inventory

1. List every comment group and its context:

   ```bash
   rg -n '^\s*//' --glob '*.go' <scope>
   ```

   The regex misses trailing comments such as
   `cmd := exec.Command(...) // #nosec G204`. Use `rg -n '//' --glob '*.go'` and
   inspect the matches, or parse with `go/parser` if the scope is large.

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
| Excuse | Justifies a workaround, shortcut, platform gap, duplicated path, or "best effort" that should be fixed. | Delete and file an issue. |
| Decision Record | Explains an important, non-obvious decision: an invariant, ordering, security boundary, compatibility rule, or tradeoff that a future editor could reasonably undo. | Move to an ADR, then delete. |

Examples from the 2026-11-20 audit:

- `// CommandKind identifies a slash command.` — tautology.
- `// It is a field so the platform helper can be stubbed` — stale after the
  test suite was removed.
- `// No line cap: pasting a long multi-line prompt must not freeze Enter` —
  excuse; the unbounded composer became issue #5.
- The suspend cycle ordering, session pinning invariant, and sanitizer allowlist
  — decision records; they became ADRs 042, 048, and 050.

Do not keep a comment just because it is interesting. If it is not important
enough to survive as an ADR, it is not important enough to stay in the source.

## Act

### Tautology and stale

Delete the comment. Re-read the surrounding declaration; if the name no longer
explains the behavior, improve the name instead of restoring the comment.

### Excuse

Delete the comment and file one issue for the underlying hack. Search first:

```bash
gh issue list -R travisennis/cake-repl --limit 50
```

File with evidence, impact, fix direction, and acceptance criteria:

```bash
gh issue create -R travisennis/cake-repl \
  --title 'refactor(area): imperative summary' \
  --body-file -
```

Reference the code path and function, not the deleted comment. One issue may
cover several copies of the same hack; do not file duplicates.

### Decision record

Allocate the next ADR number against `docs/adr`, the worklog, and active
initiatives. Write `docs/adr/NNN-short-title.md` with:

- frontmatter `status: accepted` and `date`;
- context: what problem or failure forced the decision;
- decision: the invariant, ordering, boundary, or tradeoff, including the
  rejected alternatives when they are not obvious;
- consequences: what gets better and what gets worse;
- links to the code and any tracking issue.

Then delete the comment and add the ADR to the index in `docs/adr/README.md`.
Keep the ADR short enough to read; do not copy the comment.

## Ratchet

The checker supports a partial migration. Do not try to finish the whole tree
in one pass if the scope is large.

1. Set `tools/commentcheck/limit.txt` to the current count from
   `just comment-count`. This is the one deliberate allowance for the migration.
2. Run `just comment-baseline` to record the comments that remain.
3. Delete what you classified. After each pass, lower the limit to the new
   count and run `just comment-baseline` again.
4. Never raise the limit. A PR that raises it is not a cleanup.
5. Before handoff, run `just comment-check`. It fails on a new comment, an extra
   duplicate, or a stale allowlist entry. If it fails because the baseline is
   stale, lower the limit and regenerate the baseline.

When the scope reaches zero, the allowlist is empty and the limit is `0`. That is
the endpoint ADR 040 describes.

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
the ADRs and issues back before handoff: the index must link every new ADR, and
every excuse issue must name a live code path.

## Report

Report:

- the scope and the count before and after (`just comment-count`);
- lines removed, measured from `git diff --shortstat` or
  `git diff -U0 -- '*.go' | grep -cE '^-\s*//'`;
- counts per category, with a few concrete examples;
- the ADR numbers and issue numbers created;
- the exact verification commands and any skipped check;
- the branch name, commit hashes, and worktree status.
