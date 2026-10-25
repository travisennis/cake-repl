# Guardrail: CLI, commands, and user output

**Scope.** Read before changing CLI flags (`cmd/cake-repl/main.go`), slash
commands or help text (`internal/app/commands.go`), key bindings
(`internal/app/keys.go`, `internal/app/update.go`), or terminal rendering
(`internal/ui/`).

## Compatibility surfaces

- **CLI flags.** `-cake-bin`, `-fork [<uuid>]`, `-resume [<uuid>]`,
  `-no-session`, `-model`, `-profile`, `-tools <names>`, `-no-tools`,
  `-add-dir <dir>` (repeatable), `-toolbox <dir>` (repeatable),
  `-sandbox <policy>`, `-no-skills`, `-skills <names>`,
  `-system-prompt <path>`, `-cwd`, `-inline`, `-no-color`, `-debug-log`,
  `-history-file`, `-config <path>`, `-no-config`, `-output-limit <n>`,
  `-max-timeline-items <n>`, `-tool-color`, `-version`. Names, defaults, and
  validation (mutually exclusive `-fork`/`-resume`, mutually exclusive
  `-config`/`-no-config`, uuid shape, positional args rejected) are
  user-facing. The cake pass-through flags must stay aligned with the cake
  contract. `-fork` applies to the initial fresh prompt; later prompts use
  the pinned session when one is reported. A bare `-resume` names no session:
  it opens the startup session picker (see below) instead of hydrating or
  pinning, so the two optional-value flags share one parse path. See
  [ADR 021](../adr/021-widen-resume-to-an-optional-value-that-opens-the-session-picker.md).
  `-sandbox` defaults to
  `workspace-write-interactive`, not cake's own `workspace-write`, because a
  REPL is interactive; an explicit empty value omits `--sandbox` so cake
  resolves its own policy. See
  [ADR 017](../adr/017-default-the-cake-sandbox-to-workspace-write-interactive.md).
- **Config file shape.** TOML config supports only stable REPL defaults:
  `cake-bin`, `model`, `profile`, `output-limit`, `max-timeline-items`,
  `tool-color`, and the opt-in `[compaction]` policy table (`enabled`,
  `cake-compact-bin`, `threshold-percent`, `keep-tokens`). Merge order is
  hardcoded defaults < XDG config < project-local config < CLI flags, except
  `cake-bin`: the automatically loaded `.cake-repl.toml` key is ignored with a
  plain startup warning naming the file and key, even with `-no-color`.
  Executable precedence is hardcoded `cake` < XDG/explicit `-config` <
  `-cake-bin` (including trusted relative paths). The same untrusted layer cannot
  enable compaction (`enabled = true`), set `compaction.cake-compact-bin`, or
  tune `threshold-percent`/`keep-tokens`; each is ignored with a warning, while
  `enabled = false` still applies. Explicit `-config` may select the project
  file; `-no-config` skips file values and warnings. Other project-local keys
  retain their existing behavior. See
  [ADR 006](../adr/006-project-local-config-cannot-select-the-cake-executable.md)
  and
  [ADR 023](../adr/023-compact-context-between-turns-through-an-external-compactor.md),
  narrowed by
  [ADR 024](../adr/024-ignore-project-local-compaction-tuning.md).
  Session-specific values must stay out of config.
- **Slash commands.** `/help`, `/exit` `/quit` `/q`, `/new`, `/resume <uuid>`,
  `/sessions`, `/session [copy]`, `/clear`. Keep parsing, behavior, and names
  stable. `/new`, `/resume`, and `/sessions` require an idle REPL (no live run,
  replay, or compaction); `Ctrl+N` remains the cancel-and-reset operation during
  an active run or compaction, and `/exit` cancels either before exiting.
- **Key bindings.** `Enter` (newline), `Ctrl+S` (submit), `Ctrl+C`
  (cancel/quit), `Ctrl+N` (new session), `Ctrl+U` (clear input), `Ctrl+O` (cycle all tool output
  through truncated/full/hidden), `Ctrl+Y` (copy the last assistant response's raw markdown to the system clipboard), `Up`/`Down` (history), `PgUp`/`PgDn`
  (scroll). `Ctrl+C` cancels a running task or compaction and quits only when
  idle; `Ctrl+N` cancels either and starts a new session. While the `/sessions` browser is open its keys are modal:
  `Up`/`Down`, `PgUp`/`PgDn`, and `Home`/`End` move the selection, `Enter`
  resumes the highlighted session, and `Esc`, `q`, or `Ctrl+C` close it.
- **Session browser.** `/sessions` opens an idle-only overlay that replaces the
timeline: a navigable list of this directory's past sessions from the read-only
`cake sessions list --json` command (see
[`cake-integration-and-stream-json.md`](cake-integration-and-stream-json.md)).
A bare `-resume` opens the same browser at startup, before the first prompt and
with no hydration: the picker is the startup form of the browser, sharing its
listing, keys, and selection transition. It is idle by construction (a startup
prompt submitted while it is open is consumed by the browser keys, never run),
and the startup listing is model-owned and cancellable exactly like the one
`/sessions` starts (ADR 019).
Each row shows a shortened session id, a relative age, and the sanitized first
prompt; `Enter` pins the next prompt to the selected session through the same
path as `/resume <uuid>`, and `Esc` or `q` closes the list without action. The
overlay is modal: while open it consumes keys before the composer, and it keeps
the timeline's height so the composer and status line do not move. Loading,
empty ("no sessions in <dir>"), and failure states render in place of the list;
a listing failure is never fatal and never blocks input. `-no-color` renders it
in plain text like every other surface.
- **Startup resume.** `-resume <uuid>` first invokes the read-only
  `cake --output-format stream-json replay <uuid>` command. It hydrates the
  visible timeline before the first prompt; failures show a warning and keep
  the explicit resume pin usable. A bare `-resume` hydrates nothing: it opens
  the startup session picker, pins nothing until a row is picked, and leaves the
  REPL fresh when the picker is closed. The command `main` prints on exit
  reuses the effective startup run controls (`-cwd`, `-config <path>` or
  `-no-config`, plus any of `-sandbox`, `-model`, `-profile`, `-add-dir`,
  `-toolbox`, `-tools`, `-no-tools`, `-no-skills`, `-skills`, `-system-prompt`,
  and `-cake-bin` when it was set explicitly or is non-default) after `-resume
  <id>`, so a pasted resume does not silently drop the sandbox policy or model
  the original run was started with. An explicit override that clears or
  defaults a value (`-sandbox ""`, `-model ""`, `-profile ""`, or `-cake-bin
  cake`) is re-emitted — for example `-sandbox ''` — so reloading the config on
  resume cannot reintroduce a value the original run suppressed.
- **Output rendering.** Timeline item kinds, status line, tool-block format, and
  markdown rendering for assistant messages. User and assistant items render as
  labeled conversation sections; user content retains a slim gutter at normal
  widths, while assistant response bodies have no decorative prefix so multiline
  selections copy cleanly. Narrow terminals omit the user gutter and abbreviate
  the assistant label. Reasoning renders
  as a single muted `(thinking)` marker per reasoning burst: consecutive
  `reasoning` events coalesce, any other event ends the burst, and the payload
  (e.g. a `summary`) is never shown because providers differ in what they emit.
  Reasoning and tool output remain secondary, while task starts, info,
  completions, warnings, and errors use distinct compact markers. The one-line
  status display leads with a bracketed idle/running/compacting state, followed
  by labeled session, next-run, optional
  model, optional context, and cwd context; the model is the identity cake
  reported on the stream once available (the `[[models]]` entry name, falling
  back to the provider model ID), else the `-model`/config value, and a new
  session or resume switch drops the reported identity (ADR 014). The context
  field is cake's newest complete main-agent `turn_usage` measurement, rendered
  compactly as `132k/1M` (the attempt's input tokens over the configured
  window) or as a bare `132k` when the model configures no window. Partial usage
  records are ignored, the field is omitted when no complete measurement or
  checkpoint estimate exists, and a new session or resume switch clears it so a
  stale figure never crosses into another session's status line. A replayed
  `compaction` record seeds the field from its `estimated_input.after_tokens`,
  and a live compactor success seeds it from the result's `after_tokens`;
  either is prefixed with `~` (for example `~24k/200k`) because it is an estimate
  rather than a provider measurement, and a newer complete `turn_usage`,
  replayed after the checkpoint or reported by a live turn, replaces it. Each
  checkpoint also adds exactly one `i` info marker at its physical timeline
  position, for example `context compacted 118k -> 24k, 4 records retained`, and
  never expands the checkpoint's retained tail into duplicate conversation or
  tool items. Replay and the live result share one formatter and wording; the
  live path appends its own marker at the end of the compacted turn. The session
  field names the id cake
  reported for the current session and carries none after a retarget until the
  new target reports one, while the next-run field names the pinned target. It
  pads or truncates to the terminal width. The prompt
  textarea is framed as
  a focused composer with ready/running/compacting state and concise submit,
  newline, and help hints; it names the operation that is in flight while a
  task or compaction runs, and its borders remain visible without color. While
  the TUI is running, the terminal title is `cake-repl: <absolute working directory>`,
  using the directory selected by `-cwd` or the startup directory by default.
  The title carries a `[working]` prefix while a task is in flight, a
  `[compacting]` prefix while the compactor runs, and neither when idle, because
  the status line and composer are both invisible
  when the window is unfocused, minimized, or reduced to a tab. The title never
  names a state the REPL has left: the model re-sets it on every run-state
  transition, and `main` writes the idle title sequence once the program
  returns, which covers the exits the model never observes (SIGINT, SIGTERM, a
  recovered panic). That sequence is written only to a terminal, never into
  redirected output.
  `-no-color` /
  `DefaultTheme` must keep producing usable ASCII output. Markdown renders via
  glamour with compact REPL-themed headings, quotes, links, code, and emphasis;
  the ASCII profile uses text markers and strips all ANSI. Under a color profile,
  each visible ASCII `http(s)` URL that is not markdown emphasis or
  link-label text is wrapped in a generated OSC 8 hyperlink naming that same
  URL, so a URL glamour hard-wraps across lines is openable from every visible
  fragment and the terminal's link preview shows the full destination. A URL
  with non-ASCII text stays plain, and the Ascii profile emits no hyperlinks.
  See
  [ADR 022](../adr/022-generate-osc-8-hyperlinks-for-wrapped-assistant-urls.md).
  Tool-block headers
  (tool name plus argument summary) wrap to the terminal width instead of
  truncating, with continuation lines indented under the argument column; long
  single tokens (e.g. paths and bash commands) hard-wrap so the full content
  stays visible. Multi-line bash commands show their first and last non-empty
  lines joined by an ellipsis, so setup lines (`cd`, `export`) never hide the
  actual payload.
  All timeline and status-line text is sanitized before styling: ANSI escape
  sequences are stripped, tabs expand to the next eight-column stop, and
  remaining C0/C1 controls are dropped, so tool output renders as plain text
  without its own colors. Assistant markdown is scrubbed a second time after
  glamour renders it, because decoding unescapes HTML character references
  (`&#7;`, `&#27;[2J`) into exactly those controls; the rendered markdown keeps
  only the reviewed SGR that carries the theme's styling, and `-no-color`
  strips it all. Generated OSC 8 hyperlinks are added only after that scrub, in
  the narrow form ADR 022 defines: http(s) destinations, and link text made of
  the URL's own visible characters. This is the default and cannot be turned off
  implicitly. The one exception is the opt-in `-tool-color` (config
  `tool-color`, default false): tool *output* blocks then keep SGR (`CSI ... m`)
  sequences whose parameters are in a reviewed set, with embedded resets
  followed by the enclosing style so the theme does not bleed; every other
  escape family and every other item kind stay stripped, and `-no-color` /
  `termenv.Ascii` forces stripping regardless. Note that cake runs tools with
  piped stdout, so only commands that force color (`git diff --color`,
  `rg --color=always`) emit SGR at all. See
  [`session-and-security.md`](session-and-security.md),
  [ADR 005](../adr/005-untrusted-stream-content-is-sanitized-at-the-ui-render-boundary.md),
  and [ADR 009](../adr/009-opt-in-sgr-passthrough-for-tool-output.md).
  Tool *output* truncates at the configured output limit (default 2000 bytes) on
  rune boundaries. Independently, at most the first 1 MiB of any single tool
  result is *retained* for the session, cut at ingest with the same
  "… truncated (N bytes total)" marker, so one oversized result cannot pin tens
  of MB of memory. `Ctrl+O` cycles every tool block through three session-wide
  output modes: truncated (default), full, and hidden. Full mode ignores the
  output limit and shows everything retained (including the ingest-cut marker
  when bytes were dropped). The current mode also applies
  to tool blocks added later and survives `/clear`. Only tool items are
  re-rendered on toggle; cached non-tool renders and the viewport scroll offset
  are preserved.
  `Ctrl+Y` copies the raw markdown source of the most recent `KindAssistant`
  timeline item (skipping trailing tool/completion items) to the system
  clipboard via the platform helper: `pbcopy` on macOS, `xclip`/`xsel` on
  Linux, `clip` on Windows. The copy is user-initiated and one-shot, so raw
  stream content leaves the REPL only on an explicit keypress, never
  automatically. The outcome is reported as a timeline item: an info notice
  with the copied character count on success, a warning when no assistant
  response exists yet, or an error when the clipboard helper fails.
  `/session copy` uses the same helper and reporting to copy the full session
  UUID instead, so the id can be reused for `-resume`/`-fork` while the status
  line and timeline keep the shortened form (a warning when the REPL knows
  neither a reported session id nor a resume pin yet).
- **Live-region repaint.** A change to the live region's visible content — a
  timeline push, or a key that can reshape the composer — arms a whole-frame
  repaint, scheduled at most once per 300 ms, so a screen the terminal scrolled
  under the renderer cannot keep showing a previous frame's composer or status
  rows. Nothing is written while the app is idle. The terminal's own width
  measure is not knowable from the app, so a row a terminal measures wider than
  `charmbracelet/x/ansi` (a text-default emoji such as U+261D is one cell to
  x/ansi and two cells to utf8proc-based terminals) can still wrap a frame taller
  than the screen for up to one sweep; the next repaint rewrites every row in
  order, which is also the repair a resize performs. See
  [ADR 020](../adr/020-repaint-the-frame-after-content-changes-to-heal-terminal-side-scrolls.md).
- **Inline rendering.** `-inline` (ADR 015) omits the alternate screen and
  renders the live region below the terminal height, so recent terminal
  history stays visible above the REPL. The timeline viewport is capped
  (at most `inlineViewportMax` rows and at most half the terminal), and once
  the program returns `main` erases the composer rows — TTY-guarded, like the
  title reset — so the timeline region, the resume message, and the shell
  prompt are adjacent. The alternate screen remains the default. Mouse
  reporting stays on, so the wheel scrolls the timeline and terminal history is
  reachable through the terminal's own scrollback keys. An external suspend
  parks the suspension on the alternate screen (ADR 018), so the shell's own
  job-control output never lands in the live region and the resumed frame is
  rewritten in place; the consequence is that shell output produced while the
  REPL is stopped is discarded on resume, and the resumed frame can sit above
  blank rows when the window grew while stopped.

## Required checks / test focus

- `just test` (covers `commands_test.go`, `update_test.go`, `status_test.go`,
  `toolblock_test.go`). Add cases for new flags, commands, or render kinds.
- For UI/output changes, capture a terminal screenshot for the handoff with the
  [`drive-tui`](../../.agents/skills/drive-tui/SKILL.md) skill: it launches the
  built binary in a headless tmux pane against the checked-in fake cake, so the
  procedure is repeatable and spends no money.
- For inline-mode changes, drive the real binary in a terminal at 80x24 and
  after a resize: check that history stays visible above the region, that the
  region respects the cap, and that exit leaves the timeline region and no
  composer rows. No test drives a terminal, so this capture is the evidence.
  Use the `drive-tui` skill for the mechanics, and pre-fill the pane with output
  before launching so there is history above the region to check.
- For terminal-ownership or suspend changes, drive both render modes through an
  external SIGTSTP and `fg` — idle and with a run in flight — and capture the
  resumed frame plus the pane's termios and `#{mouse_any_flag}` before, during,
  and after. The resumed frame must show one composer, one status line, and the
  conversation, and `Up` must recall history rather than echo `^[[A`.
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
- **Mistaking a green suite for proof that output is right.** Tests call
  `Update` and `View` directly and assert on strings; nothing drives the
  program, a real terminal, or a real cake stream. `just test` therefore shows
  that our own item kinds still format, not that a new event shape or a real
  stream renders correctly. For a rendering change, the evidence is the capture
  or the driven session named above, plus a note on what was not exercised.

## Related docs

- [`cake-integration-and-stream-json.md`](cake-integration-and-stream-json.md) — event source.
- [`session-and-security.md`](session-and-security.md) — `/new` `/resume`, `-debug-log`.
- [`../adr/002-config-file-for-repl-defaults.md`](../adr/002-config-file-for-repl-defaults.md) — config file decision.
- [`../adr/003-tool-output-expansion-key-binding.md`](../adr/003-tool-output-expansion-key-binding.md) — tool output expansion key binding.
- [`../adr/005-untrusted-stream-content-is-sanitized-at-the-ui-render-boundary.md`](../adr/005-untrusted-stream-content-is-sanitized-at-the-ui-render-boundary.md) — terminal-escape sanitization.
- [`../../README.md`](../../README.md) — the user-facing reference these mirror.
