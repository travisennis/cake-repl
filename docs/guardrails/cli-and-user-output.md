# Guardrail: CLI, commands, and user output

**Scope.** Read before changing CLI flags (`cmd/cake-repl/main.go`), slash
commands or help text (`internal/app/commands.go`), key bindings
(`internal/app/keys.go`, `internal/app/update.go`), or terminal rendering
(`internal/ui/`).

## Compatibility surfaces

- **CLI flags.** `-cake-bin`, `-continue`, `-resume <uuid>`, `-model`,
  `-profile`, `-cwd`, `-no-color`, `-debug-log`, `-history-file`,
  `-config <path>`, `-no-config`, `-output-limit <n>`,
  `-max-timeline-items <n>`, `-version`. Names, defaults, and validation
  (mutually exclusive `-continue`/`-resume`, uuid shape, positional args
  rejected) are user-facing. `-continue`, `-resume`, `-model`, and `-profile`
  pass-through must stay aligned with the cake contract.
- **Config file shape.** TOML config supports only stable REPL defaults:
  `cake-bin`, `model`, `profile`, `output-limit`, and
  `max-timeline-items`. Merge order is hardcoded defaults < XDG config <
  project-local config < CLI flags. Session-specific values must stay out of
  config.
- **Slash commands.** `/help`, `/exit` `/quit` `/q`, `/new`, `/continue`,
  `/resume <uuid>`, `/session`, `/clear`. Keep parsing, behavior, and names
  stable. `/new`, `/continue`, and `/resume` require an idle REPL; `Ctrl+N`
  remains the cancel-and-reset operation during an active run.
- **Key bindings.** `Enter` (newline), `Ctrl+S` (submit), `Ctrl+C`
  (cancel/quit), `Ctrl+N` (new session), `Ctrl+U` (clear input), `Ctrl+O` (cycle all tool output
  through truncated/full/hidden), `Up`/`Down` (history), `PgUp`/`PgDn`
  (scroll).
- **Output rendering.** Timeline item kinds, status line, tool-block format, and
  markdown rendering for assistant messages. User and assistant items render as
  labeled conversation sections with a slim gutter at normal widths; narrow
  widths omit the gutter and abbreviate the assistant label. Reasoning and tool
  output remain secondary, while task starts, info, completions, warnings, and
  errors use distinct compact markers. The one-line status display leads with a
  bracketed idle/running state, followed by labeled session, next-run, optional
  model, and cwd context; it pads or truncates to the terminal width. The prompt
  textarea is framed as
  a focused composer with ready/running state and concise submit, newline, and
  help hints; its borders remain visible without color. `-no-color` /
  `DefaultTheme` must keep producing usable ASCII output. Markdown renders via
  glamour with compact REPL-themed headings, quotes, links, code, and emphasis;
  the ASCII profile uses text markers and strips all ANSI. Tool-block headers
  (tool name plus argument summary) wrap to the terminal width instead of
  truncating, with continuation lines indented under the argument column; long
  single tokens (e.g. paths and bash commands) hard-wrap so the full content
  stays visible.
  Tool *output* truncates at the configured output limit (default 2000 bytes) on
  rune boundaries. `Ctrl+O` cycles every tool block through three session-wide
  output modes: truncated (default), full, and hidden. Full mode ignores the
  output limit and shows the complete raw output. The current mode also applies
  to tool blocks added later and survives `/clear`. Only tool items are
  re-rendered on toggle; cached non-tool renders and the viewport scroll offset
  are preserved.

## Required checks / test focus

- `just test` (covers `commands_test.go`, `update_test.go`, `status_test.go`,
  `toolblock_test.go`). Add cases for new flags, commands, or render kinds.
- For UI/output changes, capture a terminal screenshot for the PR.
- Manually sanity-check with `just run -no-color` when touching theming.

## Common failure modes

- **Docs drift.** Flags and config behavior must agree across `README.md`, this
  guardrail, `AGENTS.md`, and any relevant ADR. Slash commands and key bindings
  must agree across `README.md`, the `HelpText` constant in `commands.go`, and
  `AGENTS.md`/routing. Update the applicable surfaces together.
- **Color assumptions.** Don't hardcode escape codes; go through `internal/ui`
  theme/lipgloss so `-no-color` (termenv `Ascii`) still works.
- **Width/layout regressions.** The timeline caches per-item renders; width
  changes and `/clear` force a full rebuild, while `Ctrl+O` re-renders only tool
  items. Don't bypass the cache.
- **Truncation surprises.** Respect rune-safe truncation; never cut mid-rune.

## Related docs

- [`cake-integration-and-stream-json.md`](cake-integration-and-stream-json.md) — event source.
- [`session-and-security.md`](session-and-security.md) — `/new` `/continue` `/resume`, `-debug-log`.
- [`../adr/002-config-file-for-repl-defaults.md`](../adr/002-config-file-for-repl-defaults.md) — config file decision.
- [`../adr/003-tool-output-expansion-key-binding.md`](../adr/003-tool-output-expansion-key-binding.md) — tool output expansion key binding.
- [`../../README.md`](../../README.md) — the user-facing reference these mirror.
