# Guardrail: CLI, commands, and user output

**Scope.** Read before changing CLI flags (`cmd/cake-repl/main.go`), slash
commands or help text (`internal/app/commands.go`), key bindings
(`internal/app/keys.go`, `internal/app/update.go`), or terminal rendering
(`internal/ui/`).

## Compatibility surfaces

- **CLI flags.** `--cake-bin`, `--continue`, `--resume <uuid>`, `--model`,
  `--profile`, `--cwd`, `--no-color`, `--debug-log`, `--version`. Names,
  defaults, and validation (mutually exclusive `--continue`/`--resume`, uuid
  shape, positional args rejected) are user-facing. `--continue`/`--profile`
  pass-through must stay aligned with the cake contract.
- **Slash commands.** `/help`, `/exit` `/quit` `/q`, `/new`, `/continue`,
  `/resume <uuid>`, `/session`, `/clear`. Keep parsing, behavior, and names
  stable.
- **Key bindings.** `Enter` (newline), `Ctrl+S` (submit), `Ctrl+C`
  (cancel/quit), `Ctrl+U` (clear input), `Up`/`Down` (history), `PgUp`/`PgDn`
  (scroll).
- **Output rendering.** Timeline item kinds, status line, and tool-block format.
  `--no-color` / `DefaultTheme` must keep producing usable ASCII output; tool
  output truncates at `DefaultOutputLimit` (2000 bytes) on rune boundaries.

## Required checks / test focus

- `just test` (covers `commands_test.go`, `update_test.go`, `status_test.go`,
  `toolblock_test.go`). Add cases for new flags, commands, or render kinds.
- For UI/output changes, capture a terminal screenshot for the PR.
- Manually sanity-check with `just run -- --no-color` when touching theming.

## Common failure modes

- **Docs drift.** Flags, slash commands, and key bindings appear in three
  places that must agree: `README.md`, the `HelpText` constant in
  `commands.go`, and `AGENTS.md`/routing. Update all of them together.
- **Color assumptions.** Don't hardcode escape codes; go through `internal/ui`
  theme/lipgloss so `--no-color` (termenv `Ascii`) still works.
- **Width/layout regressions.** The timeline caches per-item renders; only width
  changes and `/clear` force a full rebuild. Don't bypass the cache.
- **Truncation surprises.** Respect rune-safe truncation; never cut mid-rune.

## Related docs

- [`cake-integration-and-stream-json.md`](cake-integration-and-stream-json.md) — event source.
- [`session-and-security.md`](session-and-security.md) — `/new` `/continue` `/resume`, `--debug-log`.
- [`../../README.md`](../../README.md) — the user-facing reference these mirror.
