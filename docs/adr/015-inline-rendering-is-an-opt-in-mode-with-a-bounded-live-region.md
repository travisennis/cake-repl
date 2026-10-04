---
status: proposed
date: 2026-10-04
decision-makers: Travis Ennis
---
# Inline rendering is an opt-in mode with a bounded live region

## Context and Problem Statement

`cmd/cake-repl/main.go` starts Bubble Tea with `tea.WithAltScreen()`, so the
REPL owns the full window and the terminal's own content (shell history,
previous commands) is hidden for the duration of the session and restored on
exit. A Codex-like inline mode was requested: keep recent terminal history
visible above the REPL while retaining a usable live timeline, composer, status
line, resizing, and scrolling.

Task 060 prototyped three approaches against Bubble Tea v1.3.10 in a tmux
harness (80x24, resized to 100x30 and 60x12, with 30 sentinel history lines on
screen, inspected via `tmux capture-pane` visible and scrollback, plus a real
client attached on a pty to inject SGR wheel events):

- **A. Remove `tea.WithAltScreen()` only.** The live view still takes the full
  terminal height, so the first frame scrolls the shell history into
  scrollback. Nothing is lost and resize is correct, but history is not
  visible above the REPL and the whole window remains live chrome.
- **B. Bounded inline region.** The live view is capped below the terminal
  height, so recent history stays visible above the REPL while the timeline
  viewport, composer, and status line keep working. Resize, long output, and
  exit cleanup (erase the composer/status rows, keep the timeline region,
  print the resume hint below) all behaved correctly.
- **C. Commit the transcript to terminal scrollback** with `tea.Println` and
  keep only the active region live. This is the closest Codex analogue and it
  worked (50/50 lines preserved across a resize, no duplicates or gaps), but a
  committed line wider than the terminal wraps to two rows and the renderer's
  row accounting then loses the following line (reproduced); committed output
  cannot be re-wrapped on resize or un-printed by `/clear`; `-resume` hydration
  would dump the replayed timeline into scrollback; and PgUp/PgDn are captured
  by the app, so the transcript is navigable only through terminal-native
  scrolling.

The wheel question was measured: with `tea.WithMouseCellMotion()` (today's
behavior) a wheel event is delivered to the app and the terminal does not
scroll (`#{pane_in_mode}=0`); without it, the terminal enters copy mode and
scrolls its scrollback (`#{pane_in_mode}=1`).

## Decision Drivers

- Keep recent terminal history visible above the REPL.
- Keep the timeline, composer, status line, resize handling, `/clear`, and
  scrolling usable.
- Smallest change that leaves the cake contract, stream decoding, session
  behavior, and the config shape untouched.
- Ship safely: no test or CI job drives a real terminal, so a behavior this
  terminal-dependent must be reversible and opt-in until real-terminal
  mileage exists.

## Considered Options

- A. Drop `tea.WithAltScreen()` only.
- B. Bounded inline region as an opt-in mode.
- C. Commit the completed transcript to terminal scrollback and keep only the
  active region live.

## Decision Outcome

Chosen option: B, exposed as an opt-in `-inline` flag, because it meets the
goal with the smallest change and preserves every existing timeline behavior
(viewport cache, PgUp/PgDn, wheel scrolling, `/clear`, `-max-timeline-items`),
while A fails the goal and C carries unresolved wrapping, reflow, and
scrollback-semantics costs.

### Consequences

- Good, because terminal history stays visible, the default experience is
  unchanged, and no session, stream, or config surface moves.
- Good, because the bounded region reuses the existing viewport rather than
  introducing a second rendering path.
- Bad, because the transcript lives in the app viewport, not the terminal's
  scrollback, so terminal-native search and copy do not reach it, and the
  region caps how much of a long conversation is visible at once.
- Bad, because with mouse reporting retained the wheel scrolls the timeline
  and terminal history is reachable only through the terminal's own scrollback
  bindings (Shift+PgUp / Shift+wheel, terminal-dependent).
- Bad, because inline exit leaves the timeline region on screen (only the
  composer and status rows are erased), unlike the clean restore the alternate
  screen provides.
- Neutral, because option C remains a possible future direction and would need
  its own ADR.

## More Information

- Task 060 holds the full experiment record, the recommendation, and the
  implementation scope.
- Bubble Tea v1.3.10 has no inline API; inline mode is "omit
  `tea.WithAltScreen()`". v2 makes inline the default with per-`View` alt
  screen, but the upgrade is a full API migration and is not required here.
- The renderer truncates live-view lines to the terminal width, and on Windows
  Bubble Tea v1 learns the width only at startup, so post-resize width
  handling on Windows needs a real-terminal check.
