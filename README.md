# cake-repl

A standalone terminal REPL for the [`cake`](https://github.com/travisennis/cake) CLI.

`cake-repl` treats cake as the engine: each submitted prompt spawns one
`cake --output-format stream-json` process and renders its event stream live —
assistant messages in REPL-themed markdown, thinking indicators, tool calls
grouped with their outputs, hook denials, completion stats, and
context-compaction boundaries. When `-resume` is given a UUID, cake-repl first
replays that session through
`cake --output-format stream-json replay <uuid>` to hydrate the visible timeline;
given on its own, `-resume` opens a picker of this directory's past sessions
instead.
Labeled user and assistant sections anchor the conversation. Assistant response
bodies have no decorative prefix, so multiline terminal selections can be copied
cleanly; operational events remain compact and visually distinct.
The status line leads with current idle/running state, followed by labeled
session, next-run, model, context, and working-directory context. The model
shown is the identity cake reports on the stream once available — the
`[[models]]` entry name, falling back to the provider model ID — and otherwise
the `-model`/config value. The context segment reports the newest complete
main-agent attempt's input tokens against the model's configured window when cake
reports one (for example `context: 132k/1M`), or a bare token count when the
model configures no window. It is omitted until cake reports a complete
measurement or a context checkpoint supplies an estimate, and cleared when the
session target changes. Replay seeds it from a context checkpoint's own estimate,
marked with `~` (for example `~24k/200k`) because that is cake's estimate rather
than a provider measurement; a newer complete `turn_usage`, replayed after the
checkpoint or reported by a live turn, replaces it. Once a run reports a session
ID — at `task_start` or its completion — the next prompt continues the same cake
session, whether that run succeeded, failed, or was canceled.

It never links to cake internals, parses human text output, or reads cake's
session files. The only contract is cake's stream-json NDJSON output, the
supported `replay <uuid>` and `sessions list --json` commands, and its
documented session, model, profile, tools, and add-dir flags.

## Requirements

- Go 1.27.3 or newer to build.
- `cake` installed and on `PATH`, or point at a binary with `-cake-bin`.
  Startup `-resume <uuid>` history hydration requires cake 0.1.0 or a newer build
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
cake-repl -resume                             # pick a past session to resume at startup
cake-repl -resume <uuid>                      # replay history, then resume a specific session
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

An external stop signal — `kill -TSTP <pid>` from another shell, or any
equivalent — no longer strands the terminal. cake-repl releases the terminal
before it stops and re-owns it when `fg` resumes it, so the shell keeps a
usable prompt while the REPL is stopped and the resume restores raw input,
cursor, mouse, and screen modes and repaints the same conversation: prompt
history, the timeline, and the composer are all still there. A cake run that was
in flight is paused with the REPL rather than killed, and its output arrives
after the resume. In `-inline` mode the suspension happens on the alternate
screen, so anything you run in the shell while the REPL is stopped is not kept
once you return to it.

Flags:

| Flag | Meaning |
|---|---|
| `-cake-bin <path>` | cake executable to run (default `cake`) |
| `-fork [<uuid>]` | fork the latest session, or a specific session UUID, on the first prompt |
| `-resume [<uuid>]` | with a UUID: replay visible history when supported, then resume that cake session on the first prompt; without one: pick this directory's past sessions at startup |
| `-no-session` | do not save the cake session to disk |
| `-model <name>` | passed through to cake |
| `-profile <name>` | passed through to cake |
| `-add-dir <dir>` | add a directory to cake's sandbox as read-only; repeatable |
| `-toolbox <dir>` | add a directory of user-defined cake tools; repeatable |
| `-sandbox <policy>` | sandbox policy passed through to cake (default: `workspace-write-interactive`) |
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
| `-output-limit <n>` | truncate tool output after `<n>` bytes, cut on rune boundaries (default: 2000) |
| `-max-timeline-items <n>` | limit timeline to `<n>` entries (default: no limit) |
| `-tool-color` | keep ANSI color in tool output instead of stripping it (default: strip) |
| `-version` | print version and exit |

cake-repl runs cake with the `workspace-write-interactive` sandbox policy by
default, unlike cake's own `workspace-write` default, because a REPL is
interactive and that profile adds the macOS capabilities that let a command
launch and automate other applications. Pass `-sandbox <policy>` to choose
another policy (`read-only`, `workspace-write`, `workspace-write-interactive`, or
`danger-full-access`), or `-sandbox ""` to let cake resolve its own policy from
`CAKE_SANDBOX` or its settings.

## Keybindings

| Key | Action |
|---|---|
| `Enter` | insert newline |
| `Tab` | complete slash commands |
| `Ctrl+S` | submit prompt |
| `Ctrl+C` | cancel running cake task or context compaction; quit when idle |
| `Ctrl+N` | start a new session (cancels a running task or compaction) |
| `Ctrl+U` | clear input |
| `Ctrl+O` | cycle all tool output: truncated / full / hidden |
| `Ctrl+Y` | copy the last assistant response (markdown) to the clipboard |
| `Up` / `Down` | recall prompt history (at the input's first/last line) |
| `PgUp` / `PgDn` | scroll timeline |
| `Mouse wheel` | scroll timeline |

While the `/sessions` browser is open, `Up`/`Down`, `PgUp`/`PgDn`, and
`Home`/`End` move the selection, `Enter` resumes the highlighted session, and
`Esc` or `q` closes the list.

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
| `/exit` `/quit` `/q` | exit (cancels a running task or compaction first, then exits) |
| `/new` | next prompt starts a fresh cake session |
| `/resume <uuid>` | next prompt uses `cake --resume <uuid>` |
| `/sessions` | browse this directory's past sessions and resume one |
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
- The status-line context segment comes from cake's per-turn `turn_usage`
  stream record, in live runs and replay hydration alike. It shows the newest
  complete main-agent measurement's input tokens against the configured window
  (`132k/1M`), or the bare token count when the model configures no window.
  Partial usage records are ignored, an older cake that emits no record leaves
  the segment out entirely, and a new session or a different `-resume` target
  clears the measurement until that session reports its own. When replay
  hydrates a session containing a `compaction` record, that checkpoint's
  `estimated_input.after_tokens` seeds the segment and is prefixed with `~`
  (`~24k/200k`) because it is cake's estimate, not a provider measurement. A
  newer complete `turn_usage` replaces it, whether replay emits it after the
  checkpoint or a later live turn reports it.
- A run that ends before completing — canceled, a nonzero exit, a wait error, or
  a stream that closes mid-task — is still pinned to the session id it
  announced, so the next prompt continues the session the run left behind
  instead of orphaning it.
- `/new` clears local session state; the next prompt starts fresh.
- `Ctrl+N` clears the timeline and local session state immediately. If a task
  is running, it is canceled and its remaining events are discarded. Prompt
  history, the current input draft, and model/profile settings are preserved.
- `/resume <uuid>` applies to the next prompt; once it succeeds, later prompts
  stay pinned to the same session. Choosing a different session — through
  `/resume` or the `/sessions` browser's `Enter` — drops the reported identity
  of the session you left (its session id, task id, and reported model), so the
  status line's next-run field, `/session`, `/session copy`, and the exit hint
  name the session the next prompt continues rather than the one you came from.
  Until that session reports an id of its own, `/session copy` and the exit
  hint use the pinned id you chose, and the status line shows no session id
  while its next-run field names the pin. Re-selecting the current session
  keeps its identity, and `/session`'s `last:` line still shows how the
  previous run ended.
- `/sessions` opens a navigable list of this directory's past sessions (a short
  id, a relative age, and the first prompt), read from the supported, read-only
  `cake sessions list --json` command. Arrow keys and `PgUp`/`PgDn` move the
  selection; `Enter` resumes the highlighted session exactly like
  `/resume <uuid>` (it pins the next prompt and does not replay history); `Esc`
  or `q` closes the list without action. Closing or reopening the browser stops a
  listing that is still loading, so a slow helper never keeps running after you
  are done with it, and a prompt typed after closing is submitted normally. A
  missing or older cake binary, or malformed output, shows a non-fatal warning.
  Like `/new` and `/resume`, it requires an idle REPL.
- A bare `-resume` opens that same picker at startup, before the first prompt and
  with no hydration: nothing is replayed and no session is pinned until you pick
  a row. `Enter` pins the next prompt to the highlighted session exactly like
  `/resume <uuid>`, and `Esc` or `q` leaves the REPL idle and fresh so the next
  prompt starts a new session. `-resume <uuid>` keeps the replay-and-pin startup
  path above, and `-fork` stays mutually exclusive with `-resume` in both forms.
- `/new` and `/resume` are rejected while a task or context compaction is
  running. Finish or cancel it first (Ctrl+C), or use `Ctrl+N` to cancel and
  start a new session in one action.
- On exit, cake-repl prints the command that resumes the session. The hint
  reproduces the effective startup run controls — `-cwd`, `-config <path>` or
  `-no-config`, plus any of `-sandbox`, `-model`, `-profile`, `-add-dir`,
  `-toolbox`, `-tools`, `-no-tools`, `-no-skills`, `-skills`, and
  `-system-prompt`, plus `-cake-bin` when it was set explicitly or is not the
  default `cake` — after `-resume <id>`, so a pasted resume keeps the sandbox and
  model the original run used instead of silently falling back to the startup
  defaults. An explicit override that clears or defaults a value — `-sandbox ""`,
  `-model ""`, `-profile ""`, or `-cake-bin cake` — is re-emitted (for example
  `-sandbox ''`), so reloading the config cannot reintroduce a value the original
  run suppressed.

## Context compaction (experimental)

cake-repl can compact context between prompts. After a successful turn whose
newest context measurement approaches the configured threshold, it runs the
external `cake-compact` binary, which summarizes retired history and appends an
in-place checkpoint to the same cake session. The session id does not change, so
the next prompt resumes the same session; replay still shows the raw transcript
plus one `context compacted …` marker. The feature is opt-in, off by default,
and experimental: summary quality has not been qualified.

```toml
[compaction]
enabled = true
cake-compact-bin = "cake-compact"
threshold-percent = 80
keep-tokens = 20000
```

- The REPL triggers five percentage points below `threshold-percent`, because
  the live `turn_usage` figure does not include the response and tool results
  that will be in the session. `cake-compact` re-checks the session and compacts
  only when it agrees; a no-op is silent.
- The summary is a metered model call made by `cake-compact` through cake. It
  uses the REPL's `-model` and `-profile` when set; with neither, cake's own
  default model applies.
- While compaction runs, the composer, status line, and terminal title show
  `compacting`, prompt submission is rejected, and `/new`, `/resume`, and
  `/sessions` are refused. `Ctrl+C` cancels the child; `/exit` cancels and then
  exits; `Ctrl+N` cancels and starts a new session.
- On success the status context becomes the compactor's estimate (`~24k/200k`)
  until a newer complete measurement replaces it, and the timeline shows one
  `context compacted 118k -> 24k, 4 records retained` notice. A failure,
  cancellation, or non-in-place result keeps the current session and context
  figure and shows one warning. The REPL never reads or writes cake session
  files.
- `-no-session` disables compaction with a startup warning. The automatically
  loaded project-local `.cake-repl.toml` cannot enable compaction, choose
  `cake-compact-bin`, or tune its thresholds; only XDG config or an explicit
  `-config` can. A project may only disable compaction.
- `cake-compact` must be installed, and the configured `cake` must support
  `cake sessions checkpoint`. The compactor inherits the REPL's environment and
  runs in the same `-cwd`, so `CAKE_DATA_DIR` selects the same root and a
  relative `-cake-bin` resolves in that directory. A missing binary or verb is a
  visible warning, and there is no fallback to writing session files.
- Compaction only happens between prompts. A single long tool loop that fills
  the window before the turn ends is not covered; that needs a request-boundary
  mechanism inside Cake. See
  [ADR 023](docs/adr/023-compact-context-between-turns-through-an-external-compactor.md)
  and
  [ADR 024](docs/adr/024-ignore-project-local-compaction-tuning.md).

## Config file

Persistent defaults can be set in a TOML config file. Values from the config
file are overridden by CLI flags.

### Paths

Config files are loaded from two locations. Project-local values take
precedence over XDG-level values, except that the automatic project-local layer
cannot select `cake-bin`, cannot enable `compaction`, cannot set
`compaction.cake-compact-bin`, and cannot tune `threshold-percent` or
`keep-tokens`; it may only disable compaction.

| Path | Priority |
|---|---|
| `$XDG_CONFIG_HOME/cake-repl/config.toml` (default: `~/.config/cake-repl/config.toml`) | lower |
| `.cake-repl.toml` in the current directory | higher |

Pass `--config <path>` to use a single custom config file instead of the
default paths. Pass `--no-config` to skip config file loading entirely.
`--config` and `--no-config` are mutually exclusive.

The automatically loaded `.cake-repl.toml` cannot select the executable: its
`cake-bin` key is ignored with a plain startup warning naming the file and key.
It also cannot enable context compaction, set `compaction.cake-compact-bin`, or
tune `threshold-percent`/`keep-tokens`; each is ignored with a warning. It may
disable compaction. Set the executable, enable compaction, and tune it in XDG
config, an explicitly selected `-config` file, or `-cake-bin`. Explicitly
selecting `.cake-repl.toml` with `-config` allows its `cake-bin` and compaction
values. `-no-config` skips file values and these warnings.

### Format

```toml
# Path to the cake binary (default: "cake"); XDG or explicit -config only
cake-bin = "/usr/local/bin/cake"

# Model name passed through to cake
model = "gpt-4"

# Behavior profile passed through to cake
profile = "fast"

# Truncate tool output after this many bytes, cut on rune boundaries
# (default: 2000)
output-limit = 5000

# Maximum number of timeline entries to keep (default: no limit)
max-timeline-items = 200

# Keep ANSI color in tool output instead of stripping it (default: false).
# Only tools that force color (`git diff --color`, `rg --color=always`) emit it.
tool-color = true

# Compact context between turns through the external compactor (default: off).
# XDG or explicit -config only; the automatic project-local file cannot enable
# compaction, select the compactor executable, or tune its thresholds.
[compaction]
enabled = false
cake-compact-bin = "cake-compact"
threshold-percent = 80
keep-tokens = 20000
```

### Merge order

Hardcoded defaults < XDG config < project-local config < CLI flags.
For `cake-bin`, precedence is hardcoded `cake` < XDG/explicit `-config` file <
`-cake-bin`; the automatic project-local layer is excluded. The same exclusion
applies to `compaction.enabled = true`, `compaction.cake-compact-bin`,
`threshold-percent`, and `keep-tokens`; the project-local layer may only disable
compaction. Trusted relative
executable paths keep their usual resolution. A CLI flag always wins over the
same value in the config file. Cake invocation controls such as `-fork`, `-no-session`, `-toolbox`,
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

- Startup `-resume <uuid>` history hydration requires a cake binary that
  supports `replay`; if replay is unavailable, cake-repl shows a warning and
  still lets you continue the session. The session picker — `/sessions`, and a
  bare `-resume` at startup — needs a cake binary that supports
  `sessions list --json`; without it the picker shows a non-fatal warning and
  the prompt stays available.
- Tool output is truncated at 2,000 bytes by default, cut on rune boundaries
  (configurable via `-output-limit` or config file). Independently of that
  display limit, the REPL bounds what one tool *result* pins: it retains at
  most the first 1 MiB (1,048,576 bytes) of the result's raw bytes, and a larger
  result is replaced at ingest with an owned copy of that prefix plus a
  "… truncated (N bytes total)" line, so it cannot pin tens of MB of memory —
  including a short prefix followed by megabytes of newlines. `Ctrl+O` full
  mode shows everything that was retained (including that marker); a result of
  1 MiB or less is retained verbatim. This ceiling applies to tool-result
  output only: assistant and user message text and tool arguments are kept
  whole per entry and are not capped, while reasoning payloads are discarded
  (the timeline keeps only a `(thinking)` marker).
- The timeline keeps every entry by default (`-max-timeline-items` defaults to
  no limit). This is deliberate: the ceiling above bounds what one tool result
  pins and `-output-limit` caps the rendered form, while trimming entries by
  default would silently drop scroll-back history. Message text and tool
  arguments remain unbounded per entry, so a session that streams very large
  messages still grows; set `-max-timeline-items` to bound how many entries are
  kept.
- `-history-file` stores one JSONL record per submission, capped at 1 MiB per
  record. A larger submission — for example a huge paste that begins with `/` —
  is dropped with a content-free warning rather than written, so it cannot make
  the next startup fail. If an oversized record is already in the file, startup
  loads every other entry and warns that the record was skipped; the file itself
  is never truncated by a failed read.
- Terminal control sequences are stripped from everything cake sends before it
  is drawn, so a command's own ANSI colors are not shown and tabs expand to
  eight-column stops. That includes what markdown decoding would otherwise
  introduce: character references such as `&#7;` or `&#27;[2J` are removed from
  rendered assistant messages before they are drawn. This is deliberate and is
  the default: tool output is the stdout of arbitrary commands, and escape
  sequences there could clear the screen, write your clipboard, or forge
  hyperlinks. Pass `-tool-color` (or set `tool-color = true` in the config file)
  to keep SGR color in tool output blocks; SGR only sets rendition, so it cannot
  do any of those things, and every other escape family, and every item kind
  other than tool output, stays stripped. `-no-color` still forces plain output.
  Only tools that force color emit SGR here, because cake runs them with piped
  stdout: `git diff --color` and `rg --color=always` do, a bare `rg` or `eza`
  does not. Assistant markdown is the one place the REPL writes a non-SGR
  escape of its own: under a color profile, each visible ASCII `http(s)` URL
  is wrapped in a generated OSC 8 hyperlink naming that same URL, so a link
  glamour wraps across lines can still be opened from any fragment, and the
  terminal's link preview shows the full destination. The sequence is generated
  after sanitization from the URL text itself, never passed through from the
  stream, and `-no-color` emits none. A URL with non-ASCII text, one wrapped
  in markdown emphasis, or one used as a link label stays plain text. See
  [ADR 022](docs/adr/022-generate-osc-8-hyperlinks-for-wrapped-assistant-urls.md).
  The raw bytes are still recorded when `-debug-log` is set.
- Hook events are shown only when they deny, stop, or fail; successful hook
  noise is hidden (recorded in the `-debug-log` file when one is set).
- One cake process at a time; submitting while a task runs is rejected.
- Context compaction is best-effort and between turns only. The live `turn_usage`
  measurement excludes the finished turn's response and tool results, so the
  REPL triggers on a headroom below the configured threshold and the compactor
  makes the final call. A compaction that a user cancels or that fails after its
  append may already have changed the session; the next complete `turn_usage`
  reports the real context. A single long tool loop that fills the window before
  the prompt ends is not covered.
- Suspension recovery covers catchable stop signals. Because the stop uses
  SIGSTOP, the shell reports the resumed job as `suspended (signal)`; continuing
  it in the background (`bg`) puts the REPL back in charge of the terminal while
  the shell still holds it, exactly as Bubble Tea's own suspend behaves; a bare
  SIGSTOP, or the REPL dying while it is stopped, cannot release the terminal
  first, so recover with `fg` or reset the terminal (`reset`, `stty sane`).
- `-inline` keeps the transcript in the REPL's own viewport rather than the
  terminal scrollback, so terminal search and copy do not reach it, and the
  live region is capped (at most about half the window), so a long
  conversation is scrolled with `PgUp`/`PgDn`. Mouse reporting stays on, so
  the wheel scrolls the timeline and terminal history is reachable through the
  terminal's own scrollback keys (Shift+PgUp / Shift+wheel, terminal-
  dependent).
