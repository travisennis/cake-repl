---
status: accepted
date: 2026-08-01
decision-makers: Travis Ennis
---
# Opt-in SGR passthrough for tool output

## Context and Problem Statement

ADR 005 strips every ANSI sequence unconditionally at the render boundary, so
tool output renders flat: `git diff --color`, compiler and test-runner
diagnostics, `rg`, and `eza` lose the information their color carries. SGR
(`CSI ... m`) only sets graphics rendition — it cannot move the cursor, clear
the screen, write the clipboard (`OSC 52`), or forge hyperlinks (`OSC 8`) —
and both `lipgloss.Width` and `ansi.StringWidth` ignore it, so it cannot
reintroduce the width desynchronization ADR 005 closed. The real blocker is
styling composition: a stream-embedded reset terminates the enclosing lipgloss
style mid-block.

## Decision Drivers

- The safe default must stay: stream escapes are stripped unless the user
  opts in.
- `-no-color` / `termenv.Ascii` output must remain plain regardless.
- Only tool output blocks are candidates; errors, warnings, hook text, and the
  status line stay stripped.
- The allowlist must be parser-based and bounded, not pattern-matching.

## Considered Options

1. **Keep stripping unconditionally.** Cancel the feature; flat tool output
   forever.
2. **Opt-in, tool output only, SGR allowlist.**
3. **Default-on, tool output only.**
4. **Allow SGR for all item kinds.**

## Decision Outcome

Chosen option: **2, opt-in, tool output only.**

A new `-tool-color` flag (config key `tool-color`, default `false`) enables
SGR passthrough for tool output blocks only. When disabled, behavior is
exactly ADR 005. When enabled, `ui.Sanitize` parses the stream with the
`x/ansi` parser and keeps only `CSI ... m` with validated numeric parameters;
OSC, DCS, APC, and all cursor/erase CSI finals remain stripped; parameters
outside a known-safe SGR set are rejected. Embedded resets (`CSI 0 m`,
`CSI 39/49 m`) re-emit the enclosing lipgloss style so the theme does not bleed
out of the block. `-no-color` / `termenv.Ascii` force stripping regardless of
the flag.

This supersedes in part ADR 005's unconditional-strip decision; the
session-and-security guardrail's "no opt-out" failure mode is amended to "no
implicit opt-out".

### Consequences

- Good, because users who want color get it without changing the safe default
  for everyone.
- Good, because the injection defense for every other control family is
  unchanged and parser-based.
- Neutral, because the code surface is on the attacker-influenced render path;
  tests must cover reset restoration and `-no-color`.
- Bad, because SGR passthrough must be maintained as ADR 005's sanitizer
  evolves.

## More Information

- Task 073.
- [ADR 005](005-untrusted-stream-content-is-sanitized-at-the-ui-render-boundary.md).
- `internal/ui/sanitize.go`, `internal/ui/timeline.go` (`renderTool`).

