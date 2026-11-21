---
status: accepted
date: 2026-11-20
---
# Sanitize untrusted stream content at the render boundary

## Context and Problem Statement

Timeline content comes from cake's stream. Tool output is the stdout of
arbitrary commands and the contents of arbitrary files, so it is
attacker-influenceable. Left alone, it can clear the screen, write the system
clipboard, forge a hyperlink, or desync the renderer's width math, because width
measurement ignores escape sequences and control bytes that the terminal still
acts on. Markdown decoding can also reintroduce controls through HTML character
references after an earlier scrub.

## Decision

- `internal/ui.Sanitize` is the single boundary for timeline text. It removes
  every ANSI escape sequence, expands tabs to the next eight-column stop, drops
  remaining C0 and C1 controls and DEL, replaces invalid UTF-8 with U+FFFD, and
  keeps newlines for line structure.
- `RenderItem` sanitizes every item before styling. Callers pass raw stream
  content; only the debug log sees it unmodified.
- Tool output may keep its own SGR color only when the user opts in with
  `-tool-color`, and never under the Ascii color profile. `SanitizeToolOutput`
  preserves only 7-bit SGR sequences whose parameters are in the reviewed set:
  intensity, italic, underline, reverse, strikethrough, their resets, the basic
  and bright color sets, and the extended color forms. Blink, conceal, font
  selection, and terminal-specific codes are rejected.
- A kept reset ends the enclosing lipgloss style. The renderer re-emits the
  enclosing style's opening sequence after each kept reset so the theme resumes
  for the rest of the block.
- Markdown is scrubbed after glamour decodes it, then hyperlinks are generated
  only from URL text that is already visible in the rendered output. Untrusted
  bytes never become a link.
- The status line sanitizes stream-derived fields and flattens them to one line.
  Styled local widgets, such as the spinner, are exempt.
- Hyperlink generation is tracked for replacement by #11; the sanitizer remains
  the final boundary regardless.

## Consequences

- Good: no stream byte can reach the terminal as a control sequence.
- Good: opt-in color passthrough cannot move the cursor, clear the screen, write
  the clipboard, or forge a link.
- Bad: SGR passthrough needs a reviewed allowlist and reset handling.
- Bad: markdown and hyperlinks require a second scrub after decoding.

## More Information

- [`internal/ui/sanitize.go`](../../internal/ui/sanitize.go)
- [`internal/ui/timeline.go`](../../internal/ui/timeline.go)
- [`internal/ui/markdown.go`](../../internal/ui/markdown.go)
- [`internal/ui/status.go`](../../internal/ui/status.go)
