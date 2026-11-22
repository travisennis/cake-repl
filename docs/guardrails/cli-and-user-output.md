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
  it opens the startup session picker (below) instead of hydrating or
  pinning, so the two optional-value flags share one parse path.
  `-sandbox` defaults to
  `workspace-write-interactive`, not cake's own `workspace-write`, because a
  REPL is interactive. That default applies to a fresh session; on a resumed
  target an unset `-sandbox` is omitted so cake restores the session's stored
  policy. An explicit empty value is omitted too and is treated as unset, so it
  cannot clear a stored policy.
  `-version` prints the release version followed by the embedded
  `vcs.revision` and `vcs.time` when available; a dirty build is marked
  `(modified)`.
- **Config file shape.** TOML config supports only stable REPL defaults:
  `cake-bin`, `model`, `profile`, `output-limit`, `max-timeline-items`,
  `tool-color`, the opt-in `[compaction]` policy table (`enabled`,
  `cake-compact-bin`, `max-context-tokens`, `trigger-tokens`,
  `threshold-percent`, `keep-tokens`, and the summary's `model`, `profile`,
  `reasoning-effort`), and the opt-in `[suggestions]` table (`enabled`,
  `cake-suggest-bin`, `model`, `profile`, `reasoning-effort`). The absolute
  trigger values are
  between-turn targets, not live caps: the REPL checks them only after a
  completed turn, from the newest complete `turn_usage`, so a long invocation
  can pass both before the next prompt. Merge order is
  hardcoded defaults < XDG config < project-local config < CLI flags, except
  `cake-bin`: the automatically loaded `.cake-repl.toml` key is ignored with a
  plain startup warning naming the file and key, even with `-no-color`.
  Executable precedence is hardcoded `cake` < XDG/explicit `-config` <
  `-cake-bin` (including trusted relative paths). The same untrusted layer cannot
  enable compaction (`enabled = true`), set `compaction.cake-compact-bin`, tune
  `threshold-percent`/`keep-tokens`/`max-context-tokens`/`trigger-tokens`,
  select `compaction.model`/`compaction.profile`/`compaction.reasoning-effort`,
  enable suggestions, set `suggestions.cake-suggest-bin`, or select
  `suggestions.model`/`suggestions.profile`/`suggestions.reasoning-effort`;
  each is ignored with a warning, while `enabled = false` still applies.
  Explicit `-config` may
  select the project file; `-no-config` skips file values and warnings. Other
  project-local keys retain their existing behavior.
  narrowed by
   and extended
  to the absolute trigger by

  and to summarizer selection by.
  Suggestions use the same provenance rule;
  Session-specific values, including `/model`/`/profile` selections, must stay out
  of config.
- **Slash commands.** `/help`, `/exit` `/quit` `/q`, `/new`, `/resume <uuid>`,
  `/sessions`, `/session [copy]`, `/model [<name>|default]`,
  `/profile [<name>|default]`, `/clear`. Keep parsing, behavior, and names
  stable. `/new`, `/resume`, `/sessions`, `/model`, and `/profile` require an
  idle REPL (no live or draining run, replay, or compaction); `Ctrl+N` remains
  the cancel-and-reset operation during an active run, hydration, or compaction,
  and `/exit` cancels any of them before exiting. `/model` and `/profile` omit
  the argument to show the current value, take one name to set it for the next
  prompt, and take `default` to clear it; an empty quoted argument is invalid.
  A canceled child still gates prompt submission and session retargeting until
  it has been reaped.
- **Key bindings.** `Enter` (newline), `Ctrl+S` (insert a shown suggestion
  when the input is empty; otherwise submit), `Ctrl+C`
  (cancel/quit), `Ctrl+N` (new session), `Ctrl+U` (clear input), `Ctrl+O` (cycle all tool output
  through truncated/full/hidden, starting from hidden), `Ctrl+Y` (copy the last assistant response's raw markdown to the system clipboard), `Up`/`Down` (history), `PgUp`/`PgDn`
  (scroll). `Ctrl+C` cancels a running task or compaction, cancels hydration
  and exits, and quits only when idle; `Ctrl+N` cancels a run, hydration, or
  compaction and starts a new session. A canceled run remains
  resource-busy until its result is settled. While the `/sessions` browser is open its keys are modal:
  `Up`/`Down`, `PgUp`/`PgDn`, and `Home`/`End` move the selection, `Enter`
  resumes the highlighted session, and `Esc`, `q`, or `Ctrl+C` close it.
- **Session browser.** `/sessions` opens an idle-only overlay that replaces the
timeline: a navigable list of this directory's past sessions from the read-only
`cake sessions list --json` command (
[`cake-integration-and-stream-json.md`](cake-integration-and-stream-json.md)).
A bare `-resume` opens the same browser at startup, before the first prompt and
with no hydration: the picker is the startup form of the browser, sharing its
listing, keys, and selection transition. It is idle by construction (a startup
prompt submitted while it is open is consumed by the browser keys, never run),
and the startup listing is model-owned and cancellable exactly like the one
`/sessions` starts.
Each row shows a shortened session id, a relative age, and the sanitized first
prompt; `Enter` pins the next prompt to the selected session through the same
path as `/resume <uuid>`, and `Esc` or `q` closes the list without action. The
overlay is modal: while open it consumes keys before the composer, and it keeps
the timeline's height so the composer and status line do not move. Loading,
empty ("no sessions in <dir>"), and failure states render in place of the list
and the hint offers only `esc close`; a selectable list adds the move and Enter
hints. A listing failure is never fatal and never blocks input. `-no-color`
renders it in plain text like every other surface.
- **Startup resume.** `-resume <uuid>` first invokes the read-only
  `cake --output-format stream-json replay <uuid>` command. It hydrates the
  visible timeline before the first prompt; failures show a warning and keep
  the explicit resume pin usable. The replayed `session_meta.working_directory`
  replaces the invocation directory unless `-cwd` was explicit, and the
  startup trust probe waits for that choice. A clean hydration that ends at a
  completed, non-errored turn starts one prompt suggestion when the feature is
  enabled; a failed, canceled, incomplete, or warned replay does not. A bare
  `-resume` hydrates nothing: it opens the startup session picker, pins nothing
  until a row is picked, and leaves the REPL fresh when the picker is closed.
  On a resumed target, cake-repl passes only the run controls that were
  explicitly set on the command line or changed with `/model`/`/profile`; cake
  restores the session's stored `run_config` (profile, sandbox policy, resolved
  `--add-dir`/`--toolbox`/`--mcp-config` paths, tool and skill selections, an
  explicit `--system-prompt` path, and `--no-runtime-context`) and the stored
  working directory. The hardcoded sandbox default and config-derived
  model/profile are fresh-session defaults only. For a session created in this
  process, the creation controls are retained and replayed on a follow-up turn
  until a replay reports a stored `run_config`; an older cake binary keeps the
  same sandbox and profile instead of falling back to config. Restoring the
  directory also reloads and validates its safe `.cake-repl.toml` helper
  opt-outs before the replay can start a suggestion or the next turn can start
  compaction. An unavailable replayed working directory marks the hydration
  warned and suppresses an automatic suggestion. A relative `CAKE_DATA_DIR` is
  resolved against the startup run directory before replay, so replay and the
  live turn use the same store after cwd restoration. Empty pass-through values
  are omitted and behave as unset, so they cannot clear a stored choice; start
  a new session to change it permanently. Suggestions and compaction use the
  same effective model/profile as the resumed turn, not config defaults. A
  stored `run_config` with no profile is a known empty selection, not a reason
  to fall back to the config profile. The command `main` prints on exit echoes only the
  front-end controls needed to reproduce the REPL (`-config <path>` or
  `-no-config`, `-cake-bin` when it was explicit, and `-cwd` when it was
  explicit) plus any pass-through flag that was an explicit override. A plain
  `-resume <id>` is therefore the normal hint. Display-only flags (`-inline`,
  `-no-color`, `-debug-log`, `-history-file`, `-output-limit`,
  `-max-timeline-items`, and `-tool-color`) are never emitted; boolean display
  flags do not consume the following token while the hint is reconstructed. The
  hint reflects the startup controls; a later `/model` or `/profile` change is
  not reflected.
- **Startup project-trust check.** During startup, one read-only
  `cake trust --status --json` probe inspects the effective working directory:
  the configured `-cwd`, or the session's stored working directory once a
  startup resume restores it. When it returns, an `untrusted`, `changed`,
  `invalid`, or unrecognized state adds one
  warning naming the state, authority-bearing files or changes, canonical
  project root, and the `cake trust` command that resolves it. `trusted` and
  `not_required` are silent. The warning is advisory and never gates input, so
  it normally appears before the first prompt but an immediate submission can
  arrive first. A non-zero exit, an older cake without the command, malformed
  output, or a timeout is debug-log-only. The probe is skipped when
  `CAKE_TRUST_PROJECT=1` or `CAKE_PROJECT_CONFIG=off` is set, because live
  prompts do not depend on the project's persistent trust record. The probe
  never writes the trust
  record or runs `cake trust`. See
  [cake-integration-and-stream-json.md](cake-integration-and-stream-json.md) and.
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
  status display leads with a bracketed operational state: `idle`, `running`,
  `compacting <elapsed>`, `stopping task`, `stopping compaction`,
  `loading history`, `stopping history`, `browsing sessions`, or `exiting`,
  followed by labeled session, next-run, optional
  model, optional context, and cwd context; the model is the identity cake
  reported on the stream once available, including the first fresh turn while
  it runs (the `[[models]]` entry name, falling back to the provider model ID),
  else the current `/model` selection (initialized from `-model`/config). A new
  session, resume switch, or `/model`/`/profile` change drops the reported
  identity.
  While the external prompt suggester runs, the status state appends its
  whole-second age (`[ idle · suggesting 3s ]`, or
  `[ <spinner> compacting 4s · suggesting 3s ]` when an automatic compaction
  overlaps). It is a background indicator only: it never replaces the
  operational state and never changes the composer state, hint, or input gate.
  While the between-turn compactor runs, the state segment carries its
  whole-second
  elapsed age (`[ <spinner> compacting 12s ]`), refreshed from the spinner tick
  the REPL already runs, not from a second timer; the suggester's age uses the
  same tick. The context
  field is cake's newest complete main-agent `turn_usage` measurement, rendered
  compactly as `132k/1M` (the attempt's input tokens over the configured
  window) or as a bare `132k` when the model configures no window. Partial usage
  records are ignored, the field is omitted when no complete measurement or
  checkpoint estimate exists, and a new session, resume switch, or
  `/model`/`/profile` change clears it so a stale figure never crosses into
  another session's or model's status line. A replayed
  `compaction` record seeds the field from its `estimated_input.after_tokens`,
  and a live compactor success seeds it from the result's `after_tokens`;
  either is prefixed with `~` (for example `~24k/200k`) because it is an estimate
  rather than a provider measurement, and it excludes fixed request overhead, so
  the first post-compaction status can read lower than the next provider input.
  A newer complete `turn_usage`,
  replayed after the checkpoint or reported by a live turn, replaces it. Each
  checkpoint also adds exactly one `i` info marker at its physical timeline
  position, for example `context compacted 118k -> 24k, 4 records retained`, and
  never expands the checkpoint's retained tail into duplicate conversation or
  tool items. Replay and the live result share one formatter and wording; the
  live path appends its own marker at the end of the compacted turn. A live
  compaction first shows one `context compaction started · Ctrl+C cancels`
  notice, synced in the same update that starts the child, and resolves that
  notice into the one outcome row (success marker, warning, or an explicit
  `context compaction skipped: …` row for a no-op) when the result is reaped.
  No start notice may outlive the operation. See
. The session
  field names the id cake
  reported for the current session and carries none after a retarget until the
  new target reports one, while the next-run field names the pinned target. It
  pads or truncates to the terminal width. The prompt textarea is framed as a
  focused composer whose state is `ready`, `running`, `compacting <elapsed>`,
  `stopping task`, `stopping compaction`, `loading history`, `stopping history`,
  `browsing sessions`, or `exiting`; its borders remain visible without color.
  An in-flight prompt suggestion is never one of those states: it is a
  status-line background indicator, and the composer stays `ready` because the
  request does not gate input.
  When a prompt suggestion is showing, the empty composer renders it as dim
  placeholder ghost text and the footer reads
  `Ctrl+S insert suggestion · type to hide`; the textarea value stays empty,
  so typing inserts only the user's characters. Typing and history recall hide
  the ghost and clearing the composer restores it, including after walking back
  to the saved draft; tab completion, submit, a new session, a resume, and quit
  discard it. `Ctrl+S` fills the composer with the suggestion instead of
  submitting it, and a second `Ctrl+S` submits. The filled text remains
  ordinary input: while it remains in the composer unchanged, submit bypasses
  slash-command parsing, and editing it restores normal command parsing. The
  ghost itself is never parsed as a slash command. The footer advertises only
  actions the current gate accepts: submit, newline, and help while idle;
  `Ctrl+C cancel · Ctrl+N new session` while a task or compaction is in flight;
  `Ctrl+N new session` while one drains;
  `Ctrl+C exit · Ctrl+N new session` during history hydration; the browser's
  move, resume, and close keys while it is open; and a wait notice while
  exiting. While the compactor runs the composer state carries its elapsed age,
  for example `prompt · compacting 12s`. While the TUI is running, the terminal
  title is `cake-repl: <absolute working directory>`, using the directory
  selected by `-cwd` or the startup directory by default.
  The title carries `[working]` while a task is in flight, `[compacting]` while
  the compactor runs, `[loading]` while history hydrates, `[stopping]` while a
  canceled run, replay, or compaction drains, `[browsing]` while the session
  browser is open, and `[exiting]` while the REPL waits for children to exit;
  idle has no marker. The title never
  names a state the REPL has left: the model re-sets it on every presentation
  transition, and `main` writes the idle title sequence once the program
  returns, which covers the exits the model never observes (SIGINT, SIGTERM, a
  recovered panic). That sequence is written only to a terminal, never into
  redirected output.
  `-no-color` /
  `DefaultTheme` must keep producing usable ASCII output. Markdown renders via
  glamour with compact REPL-themed headings, quotes, links, code, and emphasis;
  the ASCII profile uses text markers and strips all ANSI. Under a color profile,
  each visible ASCII `http(s)` URL, including one wrapped in markdown emphasis,
  is wrapped in a generated OSC 8 hyperlink naming that same URL, so a URL
  glamour hard-wraps across lines is openable from every visible fragment and
  the terminal's link preview shows the full destination. A visible URL that is
  only a prefix of longer source text stays unlinked; brackets and fragment
  markers (`(`, `)`, `#`, `{`, `}`, `^`, `|`) end the URL only when the source
  does not resume URL text after them. A URL with non-ASCII text stays plain,
  and the Ascii profile emits no hyperlinks.

  Tool-block headers
  (tool name plus argument summary) wrap to the terminal width instead of
  truncating, with continuation lines indented under the argument column; long
  single tokens (e.g. paths and bash commands) hard-wrap so the full content
  stays visible. Multi-line bash commands show their first and last non-empty
  lines joined by an ellipsis, so setup lines (`cd`, `export`) never hide the
  actual payload. When the arguments include an `intent` string, the header
  shows that sentence instead. `Bash`, `Edit`, and `Write` keep the technical
  summary as a dim second line; every other tool shows only the intent. Intent
  is collapsed to one line, capped at 160 cells, and sanitized with the rest of
  the header. Calls without intent render exactly as before.
  All timeline and status-line text is sanitized before styling: ANSI escape
  sequences are stripped, tabs expand to the next eight-column stop, and
  remaining C0/C1 controls are dropped, so tool output renders as plain text
  without its own colors. Assistant markdown is scrubbed a second time after
  glamour renders it, because decoding unescapes HTML character references
  (`&#7;`, `&#27;[2J`) into exactly those controls; the rendered markdown keeps
  only the reviewed SGR that carries the theme's styling, and `-no-color`
  strips it all. Generated OSC 8 hyperlinks are added only after that scrub, in
  the narrow form this guardrail defines: http(s) destinations, and link text made of
  the URL's own visible characters. This is the default and cannot be turned off
  implicitly. The one exception is the opt-in `-tool-color` (config
  `tool-color`, default false): tool *output* blocks then keep SGR (`CSI... m`)
  sequences whose parameters are in a reviewed set, with embedded resets
  followed by the enclosing style so the theme does not bleed; every other
  escape family and every other item kind stay stripped, and `-no-color` /
  `termenv.Ascii` forces stripping regardless. Note that cake runs tools with
  piped stdout, so only commands that force color (`git diff --color`,
  `rg --color=always`) emit SGR at all. See
  [`session-and-security.md`](session-and-security.md).
  Tool *output* is hidden by default: a tool call shows its header and argument
  summary only. `Ctrl+O` cycles every tool block through three session-wide
  output modes: hidden (default), truncated, and full, in the cycle order
  truncated → full → hidden → truncated; from the hidden start the first press
  shows truncated output. Truncated mode caps output at the configured output
  limit (default 2000 bytes) on rune boundaries. Independently, at most the
  first 1 MiB of any single tool result is *retained* for the session, cut at
  ingest with the same "… truncated (N bytes total)" marker, so one oversized
  result cannot pin tens of MB of memory. Full mode ignores the output limit
  and shows everything retained (including the ingest-cut marker when bytes
  were dropped). It renders the retained text without padding each line to the
  panel width, so the cached form stays proportional to the retained payload
  instead of growing by terminal width × lines; interior blank lines stay blank
  and no content is dropped. The current mode also applies to tool blocks added
  later and survives `/clear`. Only tool items are re-rendered on toggle;
  cached non-tool renders and the viewport scroll offset are preserved. A
  failed tool's message lives in its output, so hidden mode hides it until the
  user cycles output on; the stream has no error flag and its text must not be
  parsed.
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
  order, which is also the repair a resize performs.
- **Inline rendering.** `-inline`  omits the alternate screen and
  renders the live region below the terminal height, so recent terminal
  history stays visible above the REPL. The timeline viewport is capped
  (at most `inlineViewportMax` rows and at most half the terminal), and once
  the program returns `main` erases the composer rows — TTY-guarded, like the
  title reset — so the timeline region, the resume message, and the shell
  prompt are adjacent. The alternate screen remains the default. Mouse
  reporting stays on, so the wheel scrolls the timeline and terminal history is
  reachable through the terminal's own scrollback keys. An external suspend
  parks the suspension on the alternate screen, so the shell's own
  job-control output never lands in the live region and the resumed frame is
  rewritten in place; the consequence is that shell output produced while the
  REPL is stopped is discarded on resume, and the resumed frame can sit above
  blank rows when the window grew while stopped.

## Verification focus

- `just ci` for any change to flags, commands, key bindings, or render kinds.
- For UI/output changes, capture a terminal screenshot for the handoff with the
  [`drive-tui`](../../.agents/skills/drive-tui/SKILL.md) skill: it launches the
  built binary in a headless tmux pane, so the procedure is repeatable.
- For inline-mode changes, drive the real binary in a terminal at 80x24 and
  after a resize: check that history stays visible above the region, that the
  region respects the cap, and that exit leaves the timeline region and no
  composer rows. The capture is the evidence.
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
  guardrail, and `AGENTS.md`. Slash commands and key bindings
  must agree across `README.md`, the `HelpText` constant in `commands.go`, and
  `AGENTS.md`/routing. Update the applicable surfaces together.
- **Color assumptions.** Don't hardcode escape codes; go through `internal/ui`
  theme/lipgloss so `-no-color` (termenv `Ascii`) still works.
- **Width/layout regressions.** The timeline owner keeps the items and their
  cached renders in step; width changes rebuild every row, `/clear` and a new
  session drop both, and `Ctrl+O` re-renders only tool rows. Don't bypass the
  owner.
- **Truncation surprises.** Respect rune-safe truncation; never cut mid-rune.
- **Mistaking a build for proof that output is right.** Nothing here drives
  the program, a real terminal, or a real cake stream automatically. For a
  rendering change, the evidence is the capture or the driven session named
  above, plus a note on what was not exercised.

## Related docs

- [`cake-integration-and-stream-json.md`](cake-integration-and-stream-json.md) — event source.
- [`session-and-security.md`](session-and-security.md) — `/new` `/resume`, `-debug-log`.
- [`../../README.md`](../../README.md) — the user-facing reference these mirror.
