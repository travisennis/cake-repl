# Architecture

`cake-repl` is a single-binary Go terminal REPL (Bubble Tea TUI) that drives the
external [`cake`](https://github.com/travisennis/cake) CLI as an engine. Each
submitted prompt spawns one `cake --output-format stream-json` subprocess and
renders its NDJSON event stream live.

## System boundary: cake is an external process

The only contract with cake is its **command-line interface and its stream-json
NDJSON output**. cake-repl must never:

- link to cake internals or import cake source,
- read or write cake's session files,
- parse cake's human-readable text output, or
- duplicate cake's agent loop.

It communicates only through:

- `cake --output-format stream-json` (NDJSON records on stdout),
- `--continue` / `--resume <uuid>` session selection,
- `--model` / `--profile` pass-through flags,
- `--` to terminate flag parsing before the prompt argument.

This boundary is the project's reason to exist. Changes that cross it are out of
scope unless the cake contract itself has changed. See
[`docs/guardrails/cake-integration-and-stream-json.md`](docs/guardrails/cake-integration-and-stream-json.md).

## Module map

```
cmd/cake-repl/main.go    Entry point: flag parsing, validation, Bubble Tea program startup.
internal/config/         TOML config-file loading for REPL startup defaults.
internal/cake/           cake-as-engine: subprocess lifecycle + stream-json decoding.
  events.go              Typed event structs and the Event interface (the wire schema).
  parser.go              ParseLine: one NDJSON line -> typed Event (forward-compatible).
  runner.go              Start/Run: subprocess, cancellation, stderr tail, ParseError events.
internal/app/            Bubble Tea state machine (the REPL itself).
  model.go               Model struct, layout, timeline render cache.
  update.go              Event/key/command handling (the Update loop).
  session.go             sessionState: pure run-mode state machine.
  commands.go            Slash-command parsing + HelpText.
  completion.go          Tab completion for slash commands.
  keys.go                Key bindings.
  history.go             In-memory prompt history.
  view.go                Top-level View composition.
internal/ui/             Pure rendering: timeline, status line, tool blocks, theme.
internal/version/        Binary version string (overridden by release ldflags).
```

Dependency direction is one-way: `app` depends on `cake` and `ui`; `cake` and
`ui` depend on neither `app` nor each other. Keep it that way.

## Architectural invariants

- **Engine isolation.** All cake interaction lives in `internal/cake`. `app`
  consumes typed events and `cake.Run`; it never shells out to cake directly.
- **Forward-compatible decoding.** Unknown event types decode to `Unknown` and
  unknown fields are ignored, so a newer cake never breaks the stream. Malformed
  lines surface as synthetic `ParseError` events; the stream keeps flowing.
- **Pure rendering.** `internal/ui` is side-effect-free and takes data in,
  strings out. The timeline is rendered through a per-item cache; width changes
  and `/clear` trigger a full re-render, while global tool-output mode changes
  re-render only tool items.
- **Sanitize at the render boundary.** `ui.RenderItem` and `ui.StatusLine`
  strip terminal control sequences from stream content before styling it, so
  no timeline item kind can write escapes to the terminal and width math stays
  honest. Sanitization works on a copy; `internal/app` and the debug log keep
  the raw bytes. See
  [`docs/adr/005-untrusted-stream-content-is-sanitized-at-the-ui-render-boundary.md`](docs/adr/005-untrusted-stream-content-is-sanitized-at-the-ui-render-boundary.md).
- **Session state is a pure state machine.** `sessionState` (in `session.go`)
  decides the next run mode with no I/O, which is what makes the
  hijack-prevention behavior testable. After a successful task it pins future
  prompts to `--resume <session-id>`. See
  [`docs/guardrails/session-and-security.md`](docs/guardrails/session-and-security.md).
- **Config is startup-only.** Config files set stable REPL defaults before the
  TUI starts, using hardcoded defaults < XDG config < project-local config <
  CLI flags. Session-specific values stay outside the persisted config shape.
  See [`docs/adr/002-config-file-for-repl-defaults.md`](docs/adr/002-config-file-for-repl-defaults.md).
- **One cake process at a time.** Submitting while a task runs is rejected; the
  model tracks a single live `*cake.Run`.
- **Graceful cancellation.** Cancel sends SIGTERM then SIGKILL after a grace
  period (kill outright on Windows). Cancellation is classified from whether
  `cmd.Cancel` successfully signaled the process and, on POSIX, whether the
  process was signal-terminated—not from `ctx.Err()`—so a late Ctrl+C cannot
  relabel a finished run.

## Related docs

- [`README.md`](README.md) — user-facing behavior and flags.
- [`CONTRIBUTING.md`](CONTRIBUTING.md) — setup, commands, style, verification.
- [`AGENTS.md`](AGENTS.md) — agent routing.
- [`docs/guardrails/`](docs/guardrails/) — agent-facing rules by risk surface.
