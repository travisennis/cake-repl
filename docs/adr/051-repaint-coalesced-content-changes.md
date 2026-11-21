---
status: accepted
date: 2026-11-20
---
# Repaint coalesced content changes

## Context and Problem Statement

Bubble Tea's standard renderer rewrites only the lines whose text changed since
the previous frame. A terminal that measures a glyph wider than
`charmbracelet/x/ansi` wraps a full-width row, which makes the frame one physical
row taller than the screen and scrolls it. The renderer cannot see that scroll,
so skipped lines keep stale content: a previous composer border or status line
can remain on screen.

## Decision

- The model tracks `contentChanged` across updates that reshape the live region:
  viewport pushes, composer keys, and layout changes.
- A settle sweep runs every 300 ms. When content changed since the last sweep, it
  reports the current size to the program as a synthetic `tea.WindowSizeMsg`,
  which makes the renderer drop its line cache and rewrite every row. It always
  arms the next sweep, so the repair exists for the life of the program.
- The startup sweep runs immediately, so startup pays no timer delay. Every
  sweep arms the next.
- Repainting per event would rewrite the frame per event. The sweep coalesces
  every change in the interval into one repaint; an idle interval writes
  nothing.
- Layout recomputes component sizes from the terminal size and input contents.
  The timeline re-renders only when its width changes; a height change just
  resizes the viewport window. The viewport stays pinned to the bottom when it
  was already there; otherwise the previous scroll offset is restored.
- Inline mode caps the live region at 12 rows and at half the terminal, with a
  minimum of one row, so recent terminal history stays visible above the REPL.
- The composer height is between 3 and 8 rows, and the textarea model itself is
  not capped; #5 tracks restoring a bound without breaking long pastes.
- This sweep is a workaround for a renderer/terminal width mismatch; #6 tracks a
  timer-free repair.

## Consequences

- Good: a scrolled frame heals without a manual resize.
- Good: a burst of stream events costs one repaint, not one per event.
- Bad: a timer runs for the life of the program even when idle (it writes
  nothing, but it still schedules).
- Bad: the repair is indirect and depends on `WindowSizeMsg` also being a
  repaint trigger.

## More Information

- [`internal/app/update.go`](../../internal/app/update.go)
- [`internal/app/model.go`](../../internal/app/model.go)
- [`internal/ui`](../../internal/ui)
