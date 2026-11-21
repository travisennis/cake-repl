# Architecture Decision Records

This directory holds durable architecture decisions for `cake-repl`. It is the
only place prose rationale for the Go source belongs. The source itself is
comment-free by [ADR 040](040-keep-go-source-comment-free.md).

## Index

| ADR | Decision |
| --- | --- |
| [040](040-keep-go-source-comment-free.md) | Keep source comment-free; record decisions here |
| [041](041-isolate-and-bound-every-external-child-process.md) | Isolate and bound every external child process |
| [042](042-pin-the-session-target-by-announced-id.md) | Pin the session target by announced id and generation |
| [043](043-gate-operations-by-phase-and-request-identity.md) | Gate operations by phase and request identity |
| [044](044-compact-context-between-turns-only-from-trusted-config.md) | Compact context between turns only from trusted config |
| [045](045-treat-suggestions-as-a-read-only-background-hint.md) | Treat suggestions as a read-only background hint |
| [046](046-keep-project-local-config-disable-only.md) | Keep project-local config disable-only |
| [047](047-persist-prompt-history-as-bounded-jsonl.md) | Persist prompt history as bounded JSONL |
| [048](048-sanitize-untrusted-stream-content-at-the-render-boundary.md) | Sanitize untrusted stream content at the render boundary |
| [049](049-bound-retained-tool-output-at-ingest.md) | Bound retained tool output at ingest |
| [050](050-own-the-terminal-around-suspend-and-exit.md) | Own the terminal around suspend and exit |
| [051](051-repaint-coalesced-content-changes.md) | Repaint coalesced content changes |
| [052](052-render-resume-hints-from-the-startup-flag-surface.md) | Render resume hints from the startup flag surface |
| [053](053-decode-stream-json-forward-compatibly.md) | Decode stream-json forward-compatibly |
| [054](054-render-native-compaction-as-one-lifecycle.md) | Render native compaction as one lifecycle |
| [055](055-derive-all-chrome-from-one-presentation.md) | Derive all chrome from one presentation |
| [056](056-keep-justfile-recipes-safe-and-tool-pinned.md) | Keep the justfile recipes safe and tool-pinned |
| [057](057-treat-the-session-browser-as-a-bounded-read-only-overlay.md) | Treat the session browser as a bounded read-only overlay |

## Writing an ADR

Use the next three-digit number and a short kebab-case title. Keep the
frontmatter to `status` and `date`. State the context, the decision, and the
consequences. Link the issue or code path that implements it. Supersede by
adding a new ADR; do not rewrite an accepted decision in place.
