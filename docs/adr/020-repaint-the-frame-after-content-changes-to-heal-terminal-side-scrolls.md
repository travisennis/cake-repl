---
status: accepted
date: 2026-10-13
decision-makers: Travis Ennis
---
# Repaint the frame after content changes to heal terminal-side scrolls

## Context and Problem Statement

Task 110 reported a live region with stale chrome: the composer's bottom border
was missing and two status rows were on screen, the upper one holding a previous
run's state.

```
 [ running ] │ session: 5dc3d586 · next: resume 5dc3d586 · cwd: book-agent
 [ running ] │ session: 95457881 · next: resume 95457881 · cwd: book-agent
```

The cause is a mismatch between the app's frame and the terminal's screen, not
the session state or the timeline, which were correct. Bubble Tea v1.3.10's
renderer rewrites only the lines whose text changed since the previous frame
(`standard_renderer.flush` diffs against `lastRenderedLines`). Any event that
scrolls the terminal under the renderer — the screen moves up, so physical row
*i* now holds the previous frame's row *i+1* — is invisible to that diff, and
the rows it skips keep whatever the scroll moved into their place. The composer's
bottom border and the status line are exactly the rows whose text does not
change, so a single scroll leaves a previous frame's content in their slots
until something rewrites them. The report also matches the heal path: a resize
sends a `WindowSizeMsg`, which makes the renderer drop its line cache and
repaint the whole frame.

Reproduction (macOS, tmux 3.7c with utf8proc 3.2.3, 100x30, `just build`, the
checked-in `scripts/fake-cake.sh` with the `wide-emoji-row` fixture,
`-no-config -no-color -no-session`): the fixture's assistant message contains
U+261D ☝, a text-default emoji that `charmbracelet/x/ansi` measures as one cell
and utf8proc-style terminals render two cells wide. Every timeline row is padded
to the frame width, so that row is one cell wider on screen than the app's math
says, it wraps, the frame becomes one physical row taller than the screen, and
the terminal scrolls. The capture then shows the stale composer border and the
stale status row above the live ones, exactly as reported; a resize restores the
frame, and so does any plain repaint.

## Decision Drivers

- The live region must not keep showing a previous frame after the terminal
  scrolls under the renderer. The user cannot clear it except by resizing.
- The terminal's own width measure is not knowable from inside the app:
  `x/ansi` treats U+261D as one cell, utf8proc treats it as two, and terminals
  disagree about East Asian ambiguous width, emoji presentation, and unassigned
  code points. The app cannot promise a row it emits will never wrap, and it
  cannot see the resulting scroll.
- The library offers no repaint hook. Bubble Tea v1.3.10 calls `repaint()` only
  for `WindowSizeMsg`, `RestoreTerminal`, and the deprecated scroll-area
  messages; `tea.ClearScreen` erases the whole screen, which flickers in the
  alternate screen and would erase the user's own history in inline mode.
- Repairs must not rewrite the frame per stream event, and an idle REPL must
  keep writing nothing.
- Depend only on supported library surface, and record what stays unfixed.

## Considered Options

- **Prevent the wrap by reserving the worst-case width per row.** Rejected: the
  app would have to own an emoji-presentation table (no dependency exposes one),
  and every row that could be measured wider would have to be laid out one to
  *n* cells narrower — changing the frame's shape for all terminals to guard
  against some, and for the composer's borders that is impossible at all.
- **Render the frame one row shorter than the terminal.** Rejected: the probe in
  task 110 shows it does not remove the symptom, it relocates it. The frame no
  longer scrolls, but the wrap's write still lands one row low, so the stale
  content reappears in the bottom row.
- **Rewrite the frame on every update.** Rejected: it defeats the renderer's
  line diff for every event, and it does not fix a persistent over-wide row: a
  full rewrite re-wraps that row and consumes the frame's top row each time.
- **Heal on a sweep (chosen).** A periodic sweep asks the program to repaint the
  whole frame at most once per `settleDelay` (300 ms) after content changed, by
  reporting the current window size back to the program. This is the operation a
  resize already performs, so it uses supported surface only, writes nothing
  when nothing changed, and repairs the whole frame rather than the chrome
  alone.
- **Upstream fix.** Bubble Tea's open issue
  [#1808](https://github.com/charmbracelet/bubbletea/issues/1808) asks for a
  `tea.Repaint` message that drops the cell cache, re-enters the screen modes,
  and redraws; it is a v2 API and the v2 migration is out of scope here
  (ADR 015 records the same for inline rendering).

## Decision Outcome

Chosen option: heal on a sweep, because the app cannot see or prevent the
scroll, and the repaint a resize already performs is enough to restore the frame
without changing anything else about rendering.

The sweep is armed by `Init` (its first tick is immediate, so startup pays no
timer and tests that run `Init`'s commands do not block) and each sweep arms the
next at `settleDelay`. A sweep repaints when content changed since the last
sweep — a viewport push, or a key that can reshape the composer — and otherwise
only re-arms itself. The repaint is a `WindowSizeMsg` at the model's current
size: the renderer drops the line cache it diffs against and the next flush
rewrites every row in order, and the model's own handler for that message is a
pure relayout.

### Consequences

- Good, because a stale chrome row cannot outlive the next sweep: at most 300 ms
  after the frame changed, the whole frame is rewritten and every row lands
  where the app intended it.
- Good, because the heal is independent of the trigger. The pinned one is a
  width disagreement (a text-default emoji), but any terminal-side write or
  scroll — an external process writing to the tty, a suspend that resized the
  window, a resize the app learns about late — heals on the same schedule.
- Good, because nothing is written while the app is idle and nothing about the
  frame's content changes: the sweep only decides whether to repaint.
- Bad, because the app now runs a 300 ms sweep for its whole life: one no-op
  `Update` per interval when idle, and one full frame rewrite per interval while
  content is changing (about 5 KB per rewrite at 100x30, well below what the
  stream's own updates write).
- Bad, because a row the terminal measures wider still wraps, and the sweep's
  own rewrite wraps it again. In the reproduced case the frame still ends
  correct; when the over-wide row sits near the top of the frame, the rewrite
  can cost the frame's first row until that row changes. Only a terminal that
  agrees with `x/ansi` about the row's glyphs removes that class, which is what
  the upstream `Repaint`/v2 path cannot fix either.
- Neutral, because the behavior is invisible when nothing is wrong: a repaint of
  an unchanged frame looks identical, and the sweep is not user-visible
  configuration.
- Neutral, because inline mode (ADR 015) is unaffected: the heal rewrites the
  live region in place and never erases the screen, so terminal history above
  the region survives (verified at 100x30 with the same fixture).
- Neutral, because a *missed* resize (the terminal shrank and never signalled
  it) is still out of reach: the app repaints at its stale size. It heals as
  soon as any real resize arrives.

## More Information

- Task 110 holds the reproduction, the probe, and the terminal captures.
- The reproduction fixture is `internal/cake/testdata/fixtures/wide-emoji-row.ndjson`;
  `drive-tui` lists it for exactly this check.
- Measured disagreement (macOS, tmux 3.7c, utf8proc 3.2.3, 24-cell rows):
  U+261D ☝, U+270C ✌, U+270D ✍, U+26F9 ⛹, U+1F3CB 🏋, U+1F3CC 🏌, U+1F574 🕴,
  U+1F575 🕵, and U+1F590 🖐 are one cell to `charmbracelet/x/ansi` v0.11.8 and
  two cells to utf8proc; isolated regional indicators and unassigned code points
  in emoji planes disagree too. `charmbracelet/x/ansi` v0.11.8 truncates to its
  own measure, so a disagreement is always in the terminal's favour.
- Bubble Tea v1.3.10's `flush` also drops lines from the top when the frame is
  taller than its own height, which is why a frame taller than the screen is
  expected to be a bug of its own; `TestViewHeightFitsTheTerminal` pins the
  frame-height contract, with the minimum-chrome sizes excluded.
