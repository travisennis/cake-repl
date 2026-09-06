# cake-repl

A standalone terminal REPL for the [`cake`](https://github.com/travisennis/cake) CLI.

`cake-repl` treats cake as the engine: each submitted prompt spawns one
`cake --output-format stream-json` process and renders its event stream live —
assistant messages in REPL-themed markdown, thinking indicators, tool calls
grouped with their outputs, hook denials, and completion stats. When `-resume`
is supplied, cake-repl first replays that session through
`cake --output-format stream-json replay <uuid>` to hydrate the visible timeline.
Labeled user and assistant sections anchor the conversation while operational
events remain compact and visually distinct.
The status line leads with current idle/running state, followed by labeled
session, next-run, model, and working-directory context. After a successful
turn, the next prompt automatically continues the same cake session.

It never links to cake internals, parses human text output, or reads cake's
session files. The only contract is cake's stream-json NDJSON output, the
supported `replay <uuid>` command, and its documented session, model, profile,
and add-dir flags.

## Requirements

- Go 1.26.6 or newer to build.
- `cake` installed and on `PATH`, or point at a binary with `-cake-bin`.
  Startup `-resume` history hydration requires cake 0.1.0 or a newer build
  that supports `cake --output-format stream-json replay <uuid>`.

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
cake-repl -resume <uuid>                     # replay history, then resume a specific session
cake-repl -cake-bin ../cake/target/debug/cake
```

While running, cake-repl sets the terminal title to
`cake-repl: <absolute working directory>`. The `-cwd` flag selects that working
directory; otherwise cake-repl uses the directory where it was started.
Relative `-add-dir` paths resolve against that working directory, and cake
ignores paths that do not exist or are not directories.

Flags:

| Flag | Meaning |
|---|---|
| `-cake-bin <path>` | cake executable to run (default `cake`) |
| `-continue` | continue cake's latest session on the first prompt |
| `-resume <uuid>` | replay visible history when supported, then resume a specific cake session on the first prompt |
| `-model <name>` | passed through to cake |
| `-profile <name>` | passed through to cake |
| `-add-dir <dir>` | add a directory to cake's sandbox as read-only; repeatable |
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
| `Ctrl+N` | start a new session (cancels a running task) |
| `Ctrl+U` | clear input |
| `Ctrl+O` | cycle all tool output: truncated / full / hidden |
| `Ctrl+Y` | copy the last assistant response (markdown) to the clipboard |
| `Up` / `Down` | recall prompt history (at the input's first/last line) |
| `PgUp` / `PgDn` | scroll timeline |
| `Mouse wheel` | scroll timeline |

`Ctrl+Y` copies the raw markdown source of the most recent assistant message to
the system clipboard. The REPL uses the platform clipboard helpers: `pbcopy` on
macOS, `xclip`/`xsel` on Linux, and `clip` on Windows. If no assistant message
has arrived yet, or the clipboard helper is unavailable, the timeline shows a
brief notice instead.

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
- When started with `-resume <uuid>`, cake-repl first invokes cake's read-only
  `cake --output-format stream-json replay <uuid>` command and hydrates the
  visible timeline from its ordered events. Replay metadata is not shown as
  transcript text, while replayed user and assistant messages, tools, task
  boundaries, and completion records are shown.
- Replay failures (including an older cake binary without replay support) show a
  non-fatal warning. The input remains available, and the first new prompt still
  uses `cake --resume <uuid>`.
- Once cake reports a session id, future prompts are pinned to that session via
  `--resume <id>`, so another cake process creating a newer session in the
  same directory cannot hijack the conversation. If a task succeeded and cake
  never reported a session id, the fallback is `--continue`.
- A failed or canceled task is still pinned, so the next prompt continues the
  session the run left behind instead of starting a new one. A task that fails
  before reporting any session id leaves the run mode unchanged.
- `/new` clears local session state; the next prompt starts fresh.
- `Ctrl+N` clears the timeline and local session state immediately. If a task
  is running, it is canceled and its remaining events are discarded. Prompt
  history, the current input draft, and model/profile settings are preserved.
- `/continue` explicitly targets cake's latest session for the directory on
  the next prompt; once it succeeds, later prompts pin to that session.
- `/resume <uuid>` applies to the next prompt; once it succeeds, later prompts
  stay pinned to the same session.
- `/new`, `/continue`, and `/resume` are rejected while a task is running.
  Finish or cancel the task first (Ctrl+C), or use `Ctrl+N` to cancel and start
  a new session in one action.

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
`--config` and `--no-config` are mutually exclusive.

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
replaying NDJSON fixtures (successful live and replay streams, malformed lines,
non-zero exits, cancellation), so they run without a real cake binary.

One opt-in smoke test does use a real cake. It needs the `integration` build
tag and `CAKE_REAL_SMOKE=1`, and it makes a model-backed cake request that can
cost money, so it never runs from `just test` or `just ci`:

```bash
just test-real-cake
```

For all common development commands (build, test, lint, verify, release), see
[`CONTRIBUTING.md`](CONTRIBUTING.md#command-catalog).

## Release build

```bash
go build -trimpath -ldflags="-s -w" -o cake-repl ./cmd/cake-repl
```

Tagged releases are built by GoReleaser through GitHub Actions. Local release
validation is available with `just release-check`.

## Known limitations

- No session browser; `/resume` needs a UUID you already know. Startup `-resume`
  history hydration requires a cake binary that supports `replay`; if replay is
  unavailable, cake-repl shows a warning and still lets you continue the session.
- Tool output is truncated at 2,000 characters by default (configurable via
  `-output-limit` or config file). Independently of that limit, the REPL
  retains at most the first 1 MiB of any single tool result for the life of
  the session: a larger result is cut at ingest with a "… truncated (N bytes
  total)" line appended, so it cannot pin tens of MB of memory. `Ctrl+O`
  full mode shows everything that was retained (including that marker);
  output up to 1 MiB is retained verbatim.
- The timeline keeps every entry by default (`-max-timeline-items` defaults to
  no limit). This is deliberate: each entry's memory is bounded (see the tool
  output ceiling above; rendered forms are capped by `-output-limit`), and
  trimming by default would silently drop scroll-back history. Set
  `-max-timeline-items` to bound how many entries are kept.
- Terminal control sequences are stripped from everything cake sends before it
  is drawn, so a command's own ANSI colors are not shown and tabs expand to
  eight-column stops. This is deliberate and cannot be turned off: tool output
  is the stdout of arbitrary commands, and escape sequences there could clear
  the screen, write your clipboard, or forge hyperlinks. The raw bytes are
  still recorded when `-debug-log` is set.
- Hook events are shown only when they deny, stop, or fail; successful hook
  noise is hidden (recorded in the `-debug-log` file when one is set).
- One cake process at a time; submitting while a task runs is rejected.
