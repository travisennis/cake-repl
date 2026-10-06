# cake-repl

A standalone terminal REPL for the [`cake`](https://github.com/travisennis/cake) CLI.

`cake-repl` treats cake as the engine: each submitted prompt spawns one
`cake --output-format stream-json` process and renders its event stream live —
assistant messages in REPL-themed markdown, thinking indicators, tool calls
grouped with their outputs, hook denials, and completion stats. When `-resume`
is supplied, cake-repl first replays that session through
`cake --output-format stream-json replay <uuid>` to hydrate the visible timeline.
Labeled user and assistant sections anchor the conversation. Assistant response
bodies have no decorative prefix, so multiline terminal selections can be copied
cleanly; operational events remain compact and visually distinct.
The status line leads with current idle/running state, followed by labeled
session, next-run, model, and working-directory context. The model shown is the
identity cake reports on the stream once available — the `[[models]]` entry
name, falling back to the provider model ID — and otherwise the `-model`/config
value. After a successful
turn, the next prompt automatically continues the same cake session when cake
reports a session ID.

It never links to cake internals, parses human text output, or reads cake's
session files. The only contract is cake's stream-json NDJSON output, the
supported `replay <uuid>` command, and its documented session, model, profile,
tools, and add-dir flags.

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
cake-repl -fork                               # fork the latest session on the first prompt
cake-repl -fork <uuid>                        # fork a specific session on the first prompt
cake-repl -resume <uuid>                     # replay history, then resume a specific session
cake-repl -cake-bin ../cake/target/debug/cake
```

While running, cake-repl sets the terminal title to
`cake-repl: <absolute working directory>`. While a task is in flight, a
`[working]` marker prefixes that title and is removed when the run ends, so an
unfocused window or a tab still shows whether the REPL is busy. The `-cwd` flag
selects that working directory; otherwise cake-repl uses the directory where it
was started. Relative `-add-dir` paths resolve against that working directory,
and cake ignores paths that do not exist or are not directories.

By default cake-repl renders in the alternate screen, which restores the
terminal's previous contents on exit but hides them while the REPL runs.
`-inline` renders below the terminal height instead: recent terminal history
stays visible above the REPL, the timeline is capped to a bounded region (at
most about half the window), and exiting erases the composer and status rows so
the timeline region, the resume message, and the shell prompt sit next to each
other. The alternate screen stays the default.

Flags:

| Flag | Meaning |
|---|---|
| `-cake-bin <path>` | cake executable to run (default `cake`) |
| `-fork [<uuid>]` | fork the latest session, or a specific session UUID, on the first prompt |
| `-resume <uuid>` | replay visible history when supported, then resume a specific cake session on the first prompt |
| `-no-session` | do not save the cake session to disk |
| `-model <name>` | passed through to cake |
| `-profile <name>` | passed through to cake |
| `-add-dir <dir>` | add a directory to cake's sandbox as read-only; repeatable |
| `-toolbox <dir>` | add a directory of user-defined cake tools; repeatable |
| `-sandbox <policy>` | sandbox policy passed through to cake |
| `-tools <names>` | restrict cake to a comma-separated list of registered tool names; passed through to cake |
| `-no-tools` | expose no tools to cake |
| `-no-skills` | disable all cake skills |
| `-skills <names>` | load only the specified comma-separated skill names |
| `-system-prompt <path>` | use a custom cake system prompt file |
| `-cwd <path>` | run cake from this directory (default: current directory) |
| `-inline` | render below the terminal height instead of the alternate screen, keeping recent terminal history visible above the REPL |
| `-no-color` | disable styling |
| `-debug-log <path>` | append cake-repl diagnostics (raw stream lines, skipped events, exits) to a file |
| `-history-file <path>` | persist prompt history across restarts into this file |
| `-config <path>` | path to config file (overrides default paths) |
| `-no-config` | skip loading config file |
| `-output-limit <n>` | truncate tool output after `<n>` characters (default: 2000) |
| `-max-timeline-items <n>` | limit timeline to `<n>` entries (default: no limit) |
| `-tool-color` | keep ANSI color in tool output instead of stripping it (default: strip) |
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

`/session copy` copies the current cake session's full UUID to the same
clipboard, so the id can be reused with `-resume` or `-fork` without noting it
down. The status line and timeline only show it shortened.

## Slash commands

| Command | Action |
|---|---|
| `/help` | show commands and keybindings |
| `/exit` `/quit` `/q` | exit (cancels a running task first, then exits) |
| `/new` | next prompt starts a fresh cake session |
| `/resume <uuid>` | next prompt uses `cake --resume <uuid>` |
| `/session` | show session id, task id, cwd, run mode, last completion |
| `/session copy` | copy the full session id to the clipboard |
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
  same directory cannot hijack the conversation. If a task succeeds or fails
  without reporting a session id, the current run mode is left unchanged.
- The status-line model comes from the stream when cake reports one: replay
  hydration reads `session_meta.model_config` (falling back to
  `session_meta.model`), and a newer cake reports the same optional identity on
  `task_start`. Until then the `-model`/config value is shown, which is also
  what an older cake keeps showing. A new session or a switch to a different
  `-resume` target drops the reported identity.
- A failed or canceled task with a reported session ID is still pinned, so the
  next prompt continues the session the run left behind instead of starting a
  new one.
- `/new` clears local session state; the next prompt starts fresh.
- `Ctrl+N` clears the timeline and local session state immediately. If a task
  is running, it is canceled and its remaining events are discarded. Prompt
  history, the current input draft, and model/profile settings are preserved.
- `/resume <uuid>` applies to the next prompt; once it succeeds, later prompts
  stay pinned to the same session.
- `/new` and `/resume` are rejected while a task is running.
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

# Keep ANSI color in tool output instead of stripping it (default: false).
# Only tools that force color (`git diff --color`, `rg --color=always`) emit it.
tool-color = true
```

### Merge order

Hardcoded defaults < config file < CLI flags. Every layer overrides the
previous one, so a CLI flag always wins over the same value in the config
file. Cake invocation controls such as `-fork`, `-no-session`, `-toolbox`,
`-sandbox`, `-tools`, `-no-tools`, `-no-skills`, `-skills`, and
`-system-prompt` are startup-only CLI flags and are not persisted in the
config file.

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
  eight-column stops. This is deliberate and is the default: tool output is the
  stdout of arbitrary commands, and escape sequences there could clear the
  screen, write your clipboard, or forge hyperlinks. Pass `-tool-color` (or set
  `tool-color = true` in the config file) to keep SGR color in tool output
  blocks; SGR only sets rendition, so it cannot do any of those things, and
  every other escape family, and every item kind other than tool output, stays
  stripped. `-no-color` still forces plain output. Only tools that force color
  emit SGR here, because cake runs them with piped stdout: `git diff --color`
  and `rg --color=always` do, a bare `rg` or `eza` does not. The raw bytes are
  still recorded when `-debug-log` is set.
- Hook events are shown only when they deny, stop, or fail; successful hook
  noise is hidden (recorded in the `-debug-log` file when one is set).
- One cake process at a time; submitting while a task runs is rejected.
- `-inline` keeps the transcript in the REPL's own viewport rather than the
  terminal scrollback, so terminal search and copy do not reach it, and the
  live region is capped (at most about half the window), so a long
  conversation is scrolled with `PgUp`/`PgDn`. Mouse reporting stays on, so
  the wheel scrolls the timeline and terminal history is reachable through the
  terminal's own scrollback keys (Shift+PgUp / Shift+wheel, terminal-
  dependent).
