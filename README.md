# cake-repl

A standalone terminal REPL for the [`cake`](https://github.com/travisennis/cake) CLI.

`cake-repl` treats cake as the engine: each submitted prompt spawns one
`cake --output-format stream-json` process and renders its event stream live —
assistant messages, reasoning summaries, tool calls grouped with their outputs,
hook denials, and completion stats. After a successful turn, the next prompt
automatically continues the same cake session.

It never links to cake internals, parses human text output, or reads cake's
session files. The only contract is cake's stream-json NDJSON output and its
`--continue`/`--resume` session flags.

## Requirements

- Go 1.26.3 or newer to build.
- `cake` installed and on `PATH`, or point at a binary with `-cake-bin`.

## Build

```bash
go build ./cmd/cake-repl
# or
just build
```

## Install

From this checkout:

```bash
go install ./cmd/cake-repl
# or
just install
```

This installs `cake-repl` into `GOBIN`, or `GOPATH/bin` when `GOBIN` is unset.
Make sure that directory is on your `PATH`.

## Run

```bash
cake-repl                                     # fresh session on first prompt
cake-repl -continue                          # continue latest session for this directory
cake-repl -resume <uuid>                     # resume a specific session
cake-repl -cake-bin ../cake/target/debug/cake
```

Flags:

| Flag | Meaning |
|---|---|
| `-cake-bin <path>` | cake executable to run (default `cake`) |
| `-continue` | continue cake's latest session on the first prompt |
| `-resume <uuid>` | resume a specific cake session on the first prompt |
| `-model <name>` | passed through to cake |
| `-profile <name>` | passed through to cake |
| `-cwd <path>` | run cake from this directory (default: current directory) |
| `-no-color` | disable styling |
| `-debug-log <path>` | append cake-repl diagnostics (raw stream lines, skipped events, exits) to a file |
| `-history-file <path>` | persist prompt history across restarts into this file |
| `-config <path>` | path to config file (overrides default paths) |
| `-no-config` | skip loading config file |
| `-output-limit <n>` | truncate tool output after `<n>` characters (default: 2000) |
| `-max-timeline-items <n>` | limit timeline to `<n>` entries (default: no limit) |
| `-version` | print version and exit |

## Keybindings

| Key | Action |
|---|---|
| `Enter` | insert newline |
| `Tab` | complete slash commands |
| `Ctrl+S` | submit prompt |
| `Ctrl+C` | cancel running cake task; quit when idle |
| `Ctrl+U` | clear input |
| `Up` / `Down` | recall prompt history (at the input's first/last line) |
| `PgUp` / `PgDn` | scroll timeline |
| `Mouse wheel` | scroll timeline |

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
- After a successful task, future prompts are pinned to that session via
  `--resume <id>`, so another cake process creating a newer session in the
  same directory cannot hijack the conversation. If cake never reported a
  session id, the fallback is `--continue`.
- A failed or canceled task does not advance the run mode.
- `/new` clears local session state; the next prompt starts fresh.
- `/continue` explicitly targets cake's latest session for the directory on
  the next prompt; once it succeeds, later prompts pin to that session.
- `/resume <uuid>` applies to the next prompt; once it succeeds, later prompts
  stay pinned to the same session.

## Config file

Persistent defaults can be set in a TOML config file. Values from the config
file are overridden by CLI flags.

### Paths

Config files are loaded from two locations, with project-local values taking
precedence over XDG-level values:

| Path | Priority |
|---|---|
| `$XDG_CONFIG_HOME/cake-repl/config.toml` (default: `~/.config/cake-repl/config.toml`) | lower |
| `.cake-repl.toml` in the current directory | higher |

Pass `--config <path>` to use a single custom config file instead of the
default paths. Pass `--no-config` to skip config file loading entirely.

### Format

```toml
# Path to the cake binary (default: "cake")
cake-bin = "/usr/local/bin/cake"

# Model name passed through to cake
model = "gpt-4"

# Behavior profile passed through to cake
profile = "fast"

# Truncate tool output after this many characters (default: 2000)
output-limit = 5000

# Maximum number of timeline entries to keep (default: no limit)
max-timeline-items = 200
```

### Merge order

Hardcoded defaults < config file < CLI flags. Every layer overrides the
previous one, so a CLI flag always wins over the same value in the config
file.

## Tests

```bash
go test ./...
# or
just test
```

Integration tests drive the subprocess runner with fake cake shell scripts
(successful streams, malformed lines, non-zero exits, cancellation), so they
run without a real cake binary.

## Common commands

This repo includes a `justfile` for common development commands:

```bash
just build          # go build -trimpath -o bin/cake-repl ./cmd/cake-repl
just install        # go install -trimpath ./cmd/cake-repl
just test           # go test ./...
just test-race      # go test -race -cover ./...
just fmt            # go fmt ./...
just fmt-check      # fail if gofmt would change files
just tidy-check     # fail if go mod tidy would change files
just vet            # go vet ./...
just lint           # golangci-lint run
just vuln           # govulncheck ./...
just release-check  # GoReleaser config check and snapshot build
just ci             # full local/CI gate
just install-tools  # install pinned lint, vuln, and release tools
just release        # optimized local release binary at ./cake-repl
just run            # go run ./cmd/cake-repl
```

## Release build

```bash
go build -trimpath -ldflags="-s -w" -o cake-repl ./cmd/cake-repl
```

Tagged releases are built by GoReleaser through GitHub Actions. Local release
validation is available with `just release-check`.

## Known limitations

- No session browser; `/resume` needs a UUID you already know.
- Tool output is truncated at 2,000 characters by default (configurable via `-output-limit` or config file).
- Markdown in assistant messages is wrapped as plain text, not rendered.
- Hook events are shown only when they deny, stop, or fail; successful hook
  noise is hidden (recorded in the `-debug-log` file when one is set).
- One cake process at a time; submitting while a task runs is rejected.
