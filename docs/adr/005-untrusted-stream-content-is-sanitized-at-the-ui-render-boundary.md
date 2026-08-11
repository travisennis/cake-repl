---
status: accepted
date: 2026-07-28
decision-makers: Travis Ennis
---
# Untrusted stream content is sanitized at the ui render boundary

## Context and Problem Statement

Everything the timeline shows originates in cake's stream-json output: tool
output is the stdout of arbitrary commands and the contents of arbitrary files
in whatever repository cake is working in, and errors, warnings, and hook items
embed stderr and raw malformed-line snippets. Until now nothing removed
terminal control sequences from that content before it was written to the
terminal. `lipgloss` styling wraps and pads text but preserves every escape
byte, so a repository could make cake-repl clear the screen (`CSI 2J`), write
the user's system clipboard (`OSC 52`), forge a hyperlink over trusted-looking
text (`OSC 8`), or silently corrupt frame alignment, because width measurement
ignores escape sequences and bare C0 bytes that the terminal still acts on.

Assistant messages were only incidentally safe: glamour discards escapes while
rendering markdown, which is a side effect of the markdown pipeline rather than
a defense, and it disappears if that path ever changes.

## Decision Drivers

- Stream content is attacker-influenceable in the normal case, not only under
  an unusual threat model.
- Width and padding math must match what the terminal actually draws.
- Rendering must stay side-effect-free and testable, per the `internal/ui`
  boundary in `ARCHITECTURE.md`.
- Raw stream bytes must remain available exactly where the security guardrail
  already puts them: the owner-only debug log.
- No existing feature needs escape sequences from the stream to survive.

## Considered Options

1. Sanitize in `internal/app` as events are turned into timeline items. This
   catches current call sites but moves a rendering concern into the update
   loop, and it would either destroy the raw text that the debug log needs or
   require carrying two copies of every payload.
2. Sanitize per call site inside `internal/ui` (tool output here, error text
   there). This is easy to get partially right and impossible to keep right:
   each new item kind is a new hole.
3. Sanitize unconditionally at a single choke point in `ui.RenderItem` (plus
   `ui.StatusLine`, which renders cake-supplied session and model text), before
   any styling.

## Decision Outcome

Chosen option: 3, because it covers every item kind by construction, keeps
`internal/ui` pure, and leaves `internal/app` and the debug log holding the raw
bytes.

`ui.Sanitize` removes all ANSI escape sequences, expands tabs to the next
conventional eight-column stop, drops the remaining C0 and C1 control
characters and DEL, and replaces invalid UTF-8 with U+FFFD. Newline is the
only control character preserved, because callers depend on it for line
structure. Tabs are expanded rather than kept because
terminals advance them to the next tab stop while width measurement counts them
as zero cells, which is exactly the desynchronization this ADR is closing.
Tab stops are relative to column zero of each sanitized string and reset after
every newline, and the column counts rendered cells (so CJK, emoji, and
combining sequences advance it correctly), which keeps tab-separated output
column-aligned.

Sanitization is unconditional. There is no opt-out and no "render tool output
as-is" mode: an opt-out would be a per-item trust decision that nothing in the
stream carries the information to make. `RenderItem` sanitizes a copy, so the
timeline's stored items and shared tool blocks are unchanged and remain
available for re-rendering on resize.

`Status.State` is exempt because callers compose it from local styled widgets
(the spinner), not from stream content.

### Consequences

- Good, because no timeline item kind can write escape sequences to the
  terminal, and new item kinds inherit the protection without remembering to.
- Good, because width and padding math now describe what the terminal draws.
- Good, because assistant rendering no longer depends on glamour for escape
  removal.
- Neutral, because raw bytes still reach the debug log, which stays the one
  place with unmodified stream content.
- Bad, because ANSI colors produced by tools (for example `ls --color` or a
  compiler's diagnostics) are no longer shown in tool output; they render as
  plain text.
- Good, because tabs expand to the next eight-column stop, matching the
  conventional terminal default, so tab-separated output (`column -t`,
  `go test`, `df`, hand-made ASCII tables) stays column-aligned as it would in
  a normal terminal.
- Neutral, because the expansion uses the conventional eight-column default
  rather than a user-configured tab width, and the rendered text holds spaces,
  not tab bytes.

## More Information

- [`docs/guardrails/session-and-security.md`](../guardrails/session-and-security.md)
  — what may be written to disk and to the terminal.
- [`docs/guardrails/cli-and-user-output.md`](../guardrails/cli-and-user-output.md)
  — output rendering surface.
- `internal/ui/sanitize.go` and `internal/ui/sanitize_test.go`.
- Task 061.
