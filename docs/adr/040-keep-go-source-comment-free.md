---
status: accepted
date: 2026-11-20
---
# Keep Go source and the justfile comment-free

## Context and Problem Statement

The Go source had 803 comment groups and 2,620 prose comment lines in 11,148
lines of code. A full audit on 2026-11-20 found four kinds:

- **Tautologies** that repeated the identifier or the next line.
- **Stale** comments that referenced removed tests, task numbers, or
  implementation that had changed.
- **Excuses** that explained a workaround instead of fixing it.
- **Decision records** that carried non-obvious reasoning worth keeping.

Comments are not compiled, not checked, and not visible at the call site. The
same hack could be justified in three files, then copied because the comment
made it look settled.

## Decision

Go files and the `justfile` contain no prose comments. Only machine directives
are allowed:

- build constraints (`//go:build`, `// +build`);
- tool directives (`//go:`, `//line`, `//export`, `//extern`, `//lint:`,
  `//revive:`);
- lint suppressions (`//nolint`, `// #nosec`);
- generated-code markers.

Durable decisions move to this ADR directory. Workarounds are deleted and filed
as issues. Tautologies and stale comments are deleted outright.

`tools/commentcheck` enforces the rule. It parses every Go file and scans
`justfile` lines, ignores the directives above and `#!/` shebangs, and fails on
anything else. It runs as a dependency of `just lint`, so it is part of CI and
the pre-commit hook.

The checker supports a ratchet for a partial migration:

- `tools/commentcheck/allowlist.txt` lists the comments still permitted, keyed
  by path and normalized text hash so line movement is not a new comment.
- `tools/commentcheck/limit.txt` is the maximum permitted total.
- `just comment-baseline` rewrites the allowlist from the current tree and
  refuses to exceed the limit.
- A new comment, an extra duplicate, or a stale allowlist entry fails.
- Lowering the limit is a deliberate, reviewable change. Raising it is not part
  of a cleanup.

The 2026-11-20 audit started with an empty allowlist and a limit of `0`.

## Consequences

- Good: 2,620 lines left the source, about 23.5% of the Go tree.
- Good: a workaround cannot hide behind a paragraph of justification.
- Good: the remaining decisions have one searchable home.
- Bad: local rationale is gone; a reader must follow an ADR link from the
  owning package when a subtle invariant matters.
- Neutral: 14 machine directives remain because removing them would break the
  build, lint, or security scanner.

## More Information

- Checker: [`tools/commentcheck`](../../tools/commentcheck/main.go)
- Workarounds filed from the audit: #5, #6, #7, #8, #9, #10, #11, #12, #13.
- Decision records consolidated into ADRs 041--057.
