---
status: accepted
date: 2026-07-18
---
# Tool output expansion key binding

## Context and Problem Statement

Tool output is truncated at the configured `-output-limit` (default 2000
characters) with a marker showing the original size. The full output is stored
in the item data, but users have no way to view it without re-running cake with
a higher limit. We need an in-place expansion control that is discoverable,
per-item, and does not re-render the whole timeline.

## Decision Drivers

- Users need to inspect long tool outputs on demand.
- The control must not interfere with normal timeline scrolling.
- The state must be per-item, not global, so expanding one tool does not affect
  others.
- The key binding must follow an established convention (acai uses `Ctrl+O`).
- The implementation must keep `-no-color` usable and avoid full timeline
  rebuilds on toggle.

## Considered Options

1. **Global output limit override** — a single key to switch the whole timeline
   between truncated and full output. Rejected: it changes every tool block at
   once, which is noisy when only one output is relevant.
2. **Per-item clickable toggle** — mouse-click on a tool block to expand it.
   Rejected: the REPL already avoids mouse-centric interactions; `Ctrl+O` is
   faster and terminal-portable.
3. **`Ctrl+O` cycles the most recent tool block through three states** — no
   output, truncated (default), and full — Accepted.

## Decision Outcome

Chosen option: 3, because it gives per-item control without re-rendering the
full timeline, follows a known convention, and keeps the default behavior
unchanged.

The cycle order is **no output → truncated → full → no output**. New tool
blocks start in **truncated** so existing behavior is preserved. Expansion to
**full** ignores the configured `-output-limit` and shows the complete raw
output. The key binding is added to the REPL key map and mirrored in the
`/help` text, `README.md`, and `AGENTS.md`.

### Consequences

- Good, because users can inspect long outputs without restarting the REPL.
- Good, because only the toggled item is re-rendered; the viewport offset is
  preserved.
- Good, because the default truncated view is unchanged.
- Neutral, because a new key binding must be documented and maintained as a
  compatibility surface.

## More Information

- Task: [010](../../.ahm/tasks/completed/010.md)
- Guardrail: [`docs/guardrails/cli-and-user-output.md`](../guardrails/cli-and-user-output.md)

## Superseding Decision (2026-07-18)

Task [048](../../.ahm/tasks/completed/048.md) supersedes the per-item portion
of this decision. `Ctrl+O` now cycles one session-wide mode in the order
**truncated → full → hidden → truncated**. The mode applies to every existing
tool block and to tool blocks added later, remains in memory across `/clear`,
and has no persisted or configurable startup default.

The global behavior replaces the `ToolBlock` per-item mode because changing
only the most recent block could appear to do nothing when that block was off
screen or its output fit below the truncation limit. A toggle re-renders every
tool item while retaining cached renders for non-tool items. Viewport behavior
is unchanged: it stays pinned at the bottom or restores the previous offset.
