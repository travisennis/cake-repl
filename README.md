# cake-repl

A standalone terminal REPL for the [`cake`](../cake) CLI.

`cake-repl` treats cake as the engine: each submitted prompt spawns one
`cake --output-format stream-json` process and renders its event stream live —
assistant messages, reasoning summaries, tool calls grouped with their outputs,
hook denials, and completion stats. After a successful turn, the next prompt
automatically continues the same cake session.

It never links to cake internals, parses human text output, or reads cake's
session files. The only contract is cake's stream-json NDJSON output and its
`--continue`/`--resume` session flags.

## Requirements

- Go 1.26 or newer to build.
- `cake` installed and on `PATH`, or point at a binary with `--cake-bin`.

## Build

```bash
go build ./cmd/cake-repl
```

## Install

From this checkout:

```bash
go install ./cmd/cake-repl
```

This installs `cake-repl` into `GOBIN`, or `GOPATH/bin` when `GOBIN` is unset.
Make sure that directory is on your `PATH`.

## Run

```bash
cake-repl                                     # fresh session on first prompt
cake-repl --continue                          # continue latest session for this directory
cake-repl --resume <uuid>                     # resume a specific session
cake-repl --cake-bin ../cake/target/debug/cake
```

Flags:

| Flag | Meaning |
|---|---|
| `--cake-bin <path>` | cake executable to run (default `cake`) |
| `--continue` | continue cake's latest session on the first prompt |
| `--resume <uuid>` | resume a specific cake session on the first prompt |
| `--model <name>` | passed through to cake |
| `--profile <name>` | passed through to cake |
| `--cwd <path>` | run cake from this directory (default: current directory) |
| `--no-color` | disable styling |
| `--debug-log <path>` | append cake-repl diagnostics (raw stream lines, exits) to a file |

## Keybindings

| Key | Action |
|---|---|
| `Enter` | insert newline |
| `Ctrl+S` | submit prompt |
| `Ctrl+C` | cancel running cake task; quit when idle |
| `Ctrl+U` | clear input |
| `PgUp` / `PgDn` | scroll timeline |

## Slash commands

| Command | Action |
|---|---|
| `/help` | show commands and keybindings |
| `/exit` `/quit` `/q` | exit (cancels a running task first, then exits) |
| `/new` | next prompt starts a fresh cake session |
| `/continue` | next prompt uses `cake --continue` |
| `/resume <uuid>` | next prompt uses `cake --resume <uuid>` |
| `/session` | show session id, task id, cwd, run mode, last completion |
| `/clear` | clear the timeline (session state is kept) |

## Session behavior

- A fresh start uses no session flag.
- After a successful task, future prompts default to `--continue`.
- A failed or canceled task does not advance the run mode.
- `/new` clears local session state; the next prompt starts fresh.
- `/resume <uuid>` applies to the next prompt; once it succeeds, later prompts
  default back to `--continue`.

## Tests

```bash
go test ./...
```

Integration tests drive the subprocess runner with fake cake shell scripts
(successful streams, malformed lines, non-zero exits, cancellation), so they
run without a real cake binary.

## Common commands

This repo includes a `justfile` for common development commands:

```bash
just build      # go build ./cmd/cake-repl
just install    # go install ./cmd/cake-repl
just test       # go test ./...
just fmt        # gofmt -w cmd internal
just vet        # go vet ./...
just release    # optimized local release binary
just run        # go run ./cmd/cake-repl
```

## Release build

```bash
go build -trimpath -ldflags="-s -w" -o cake-repl ./cmd/cake-repl
```

## Known limitations

- No session browser; `/resume` needs a UUID you already know.
- Tool output is truncated at 2,000 characters with no expansion control yet.
- Markdown in assistant messages is wrapped as plain text, not rendered.
- Hook events are shown only when they deny, stop, or fail; successful hook
  noise is hidden (visible with `--debug-log`).
- One cake process at a time; submitting while a task runs is rejected.
