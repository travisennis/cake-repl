---
status: accepted
date: 2026-11-03
decision-makers: Travis Ennis
---
# Default tool output to hidden

## Context and Problem Statement

ADR 003 introduced `Ctrl+O` and a session-wide cycle of truncated → full →
hidden. Its later superseding decision kept new tool blocks in truncated and
recorded that the mode has no persisted or configurable startup default. In
practice, every session therefore opens showing up to `-output-limit` bytes
(2000 by default) of every tool result, even though the common case is a user
who wants to see what ran and will expand only the result they care about. The
hidden state exists for exactly that, but it sits last in the cycle and is never
the starting point.

## Decision Drivers

- The default view should be quiet: one header per tool call, no output body.
- Output must stay retained, so `Ctrl+O` can still reveal truncated and full
  content for the whole session, including blocks that arrived before the
  toggle.
- No new configuration or persisted state; the default is a product decision.
- The existing cycle order and retention policy (ADR 010) stay unchanged.
- A failure's text lives in `function_call_output.output`, and the cake contract
  forbids parsing that text, so hidden will also hide failure messages.

## Considered Options

- **Keep truncated as the startup default.** Rejected: it makes the noisy view
  the default and leaves hidden as an easy-to-miss third state.
- **Add a `-tool-output` flag or config key for the startup mode.** Rejected: a
  new compatibility surface for little benefit; the mode is already one key
  press away, and the decision is a default, not a per-user setting.
- **Start in hidden (chosen).** It is a one-field change at model construction,
  keeps the cycle and retention intact, and makes the quiet view the default.

## Decision Outcome

Chosen option: start every REPL session in hidden mode.

- `app.New` initializes the session-wide mode to `ui.ToolOutputHidden`.
- `ui.ToolOutputMode`'s zero value remains `ToolOutputTruncated`, so a renderer
  caller that does not set a mode keeps the old behavior; only the REPL's
  startup mode changes.
- The cycle order is unchanged: truncated → full → hidden → truncated. From the
  hidden startup state, the first `Ctrl+O` shows truncated output, the second
  shows full output, and the third hides it again.
- Hidden mode suppresses the output body, the running line, and the empty-output
  line; it still renders the tool header and its argument summary. A failed
  tool therefore shows only its header until the user cycles to truncated or
  full. This is accepted because the stream has no error flag and parsing the
  output text is outside the cake contract.
- Output retention is unchanged: the ingest ceiling (ADR 010) still applies, and
  toggling to truncated or full shows the retained content.
- The mode remains in memory for the session, survives `/clear`, and is never
  persisted or configurable. This partially supersedes ADR 003's clause that new
  tool blocks start in truncated; the cycle and key binding are unchanged.

### Consequences

- Good: sessions open with one scannable line per tool call instead of up to
  2000 bytes of each result.
- Good: hidden mode skips output sanitization and rendering, so the default view
  does less work.
- Neutral: a failed tool is quiet by default; `Ctrl+O` reveals its output.
- Neutral: users who prefer output-by-default press `Ctrl+O` once, and the mode
  then applies to every tool block for the rest of the session.

## More Information

- Task: 132, in the user-level `ahm` store.
- Guardrail: [`docs/guardrails/cli-and-user-output.md`](../guardrails/cli-and-user-output.md)
- Partially supersedes [ADR 003](003-tool-output-expansion-key-binding.md)'s
  truncated-start default; the cycle, key binding, and session-wide scope are
  unchanged.
- Retention policy: [ADR 010](010-bound-per-tool-result-retention-and-record-the-unbounded-timeline-default.md).
