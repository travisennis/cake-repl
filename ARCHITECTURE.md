# Architecture

`cake-repl` is a single-binary Go terminal REPL (Bubble Tea TUI) that drives the
external [`cake`](https://github.com/travisennis/cake) CLI as an engine. Each
submitted prompt spawns one `cake --output-format stream-json` subprocess and
renders its NDJSON event stream live. When startup receives `-resume <uuid>`, a
read-only replay stream hydrates the local timeline before the first prompt; a
bare `-resume` opens a picker of this directory's sessions instead, and hydrates
nothing until one is chosen. When opted in, a completed turn over its context
threshold is followed by one external `cake-compact` run that appends an
in-place checkpoint to the same session (ADR 023), and a completed turn may
also ask the external `cake-suggest` binary for the next prompt and show it as
ghost text in the composer (ADR 028).

## System boundary: cake is an external process

The only contract with cake is its **command-line interface and its stream-json
NDJSON output**. cake-repl must never:

- link to cake internals or import cake source,
- read or write cake's session files,
- parse cake's human-readable text output, or
- duplicate cake's agent loop.

It communicates only through:

- `cake --output-format stream-json` (NDJSON records on stdout),
- `cake --output-format stream-json replay <uuid>` for read-only transcript
  hydration,
- `cake sessions list --json` for read-only session listing (versioned JSON
  envelope; see
  [`docs/adr/016`](docs/adr/016-allow-read-only-cake-sessions-list-for-the-session-browser.md)),
- `--resume <uuid>` session selection, with initial `--fork [<uuid>]` support and
  a bare `--resume` selecting the session interactively at startup,
- `--no-session`, `--model` / `--profile` pass-through flags,
- `--add-dir <dir>` read-only sandbox directories (repeatable),
- `--toolbox <dir>` user-defined tool directories (repeatable),
- `--sandbox <policy>` (default `workspace-write-interactive`, not cake's
  `workspace-write`), `--tools <names>` / `--no-tools`,
- `--no-skills` / `--skills <names>`, and `--system-prompt <path>`,
- `--` to terminate flag parsing before the prompt argument.

One non-cake executable is also part of the boundary: the opt-in
`cake-compact compact -json -threshold … <session-id>` compactor. The REPL never
passes `-fork`, never reads or writes the session file, and decodes only the
one-document result. See
[`docs/adr/023-compact-context-between-turns-through-an-external-compactor.md`](docs/adr/023-compact-context-between-turns-through-an-external-compactor.md).
A second opt-in external executable is the prompt suggester:
`cake-suggest suggest -json … -- <session-id>`. It is read-only and returns one
suggestion document; the REPL never reads a session file and never parses the
suggester's human output. It starts alongside an automatic compaction and reads
through cake's read-only replay stream, which tolerates the compactor's
concurrent append. See
[`docs/adr/028-offer-after-turn-prompt-suggestions-from-an-external-suggester.md`](docs/adr/028-offer-after-turn-prompt-suggestions-from-an-external-suggester.md)
and
[`docs/adr/031-run-after-turn-suggestions-alongside-compaction.md`](docs/adr/031-run-after-turn-suggestions-alongside-compaction.md).

This boundary is the project's reason to exist. Changes that cross it are out of
scope unless the cake contract itself has changed. See
[`docs/guardrails/cake-integration-and-stream-json.md`](docs/guardrails/cake-integration-and-stream-json.md).

## Module map

```
cmd/cake-repl/main.go    Entry point: flag parsing, validation, Bubble Tea program startup.
cmd/cake-repl/suspend.go Terminal release/restore around an external SIGTSTP (POSIX).
internal/config/         TOML config-file loading for REPL startup defaults.
internal/childproc/      POSIX session boundary shared by every external child (ADR 027).
internal/cake/           cake-as-engine: subprocess lifecycle + stream-json decoding.
  events.go              Typed live and replay event structs and the Event interface (the wire schema).
  parser.go              ParseLine: one NDJSON line -> typed Event (forward-compatible).
  runner.go              Start/Replay: subprocess lifecycle, group cancellation, stderr tail, ParseError events.
  process_unix.go        Process-group signals and POSIX suspend hooks.
  process_windows.go     Windows process fallbacks.
  registry.go            Live/replay run registry for suspend coordination.
internal/compact/        External compactor: one documented subprocess and its result document.
  compact.go             Args, Compact: bounded subprocess lifecycle + document decoding.
internal/suggest/        External prompt suggester: one documented subprocess and its result document.
  suggest.go             Args, Suggest: bounded subprocess lifecycle + document decoding.
internal/app/            Bubble Tea state machine (the REPL itself).
  model.go               Model struct, layout, timeline render cache.
  update.go              Event/key/command handling (the Update loop).
  session.go             sessionState: pure run-mode state machine.
  compaction.go          Between-turn trigger and ceiling policy, gate, and compactor result handling (ADR 023, ADR 025).
  commands.go            Slash-command parsing + HelpText.
  completion.go          Tab completion for slash commands.
  keys.go                Key bindings.
  history.go             In-memory prompt history.
  sessions.go            /sessions browser state, rows, and layout.
  view.go                Top-level View composition.
internal/ui/             Pure rendering: timeline, status line, tool blocks, theme.
internal/version/        Binary version plus embedded revision and commit time.
```

Dependency direction is one-way: `app` depends on `cake`, `compact`,
`suggest`, and `ui`; `cake`, `compact`, `suggest`, and `ui` depend on neither
`app` nor each other. `cake`, `compact`, and `suggest` also use the leaf
`internal/childproc` package for the shared session boundary. Keep it that way.

## Architectural invariants

- **Engine isolation.** All cake interaction lives in `internal/cake`. `app`
  consumes typed events and `cake.Run`; it never shells out to cake directly.
- **Forward-compatible decoding.** Unknown event types decode to `Unknown` and
  unknown fields are ignored, so a newer cake never breaks the stream. Malformed
  lines surface as synthetic `ParseError` events; the stream keeps flowing.
- **Replay stays read-only.** Startup `-resume <uuid>` hydrates the timeline from
  cake's supported replay stream before accepting a new prompt. Replay runs in a
  separate hydration state, so it does not appear as a live task; failures warn
  and preserve the explicit `--resume <uuid>` next-run pin. A bare `-resume`
  never reaches this path: it opens the session picker, which runs only the
  read-only session listing, so no replay runs before the first prompt. A
  session containing a context checkpoint renders one `compaction` marker at the
  record's physical position and never expands the checkpoint's retained tail
  into duplicate timeline items. See
  [`docs/adr/011-replay-resumed-sessions-through-cake-stream-json.md`](docs/adr/011-replay-resumed-sessions-through-cake-stream-json.md).
- **Session listing is read-only.** `cake sessions list --json` is invoked only
  from `internal/cake`, with the same working directory as a live prompt, to list
  that directory's sessions. It is informational (list and select, never
  mutate), decodes forward-compatibly, and degrades to an empty or warning state
  on any failure rather than a fatal error. It is also a model-owned, cancellable
  operation: closing or replacing the browser and quitting cancel the in-flight
  listing, a superseded result is dropped by request identity, and `ListSessions`
  bounds its teardown with `WaitDelay` so a stuck helper cannot hang the REPL. See
  [`docs/adr/016-allow-read-only-cake-sessions-list-for-the-session-browser.md`](docs/adr/016-allow-read-only-cake-sessions-list-for-the-session-browser.md)
  and
  [`docs/adr/019-cancel-and-bound-the-read-only-session-listing.md`](docs/adr/019-cancel-and-bound-the-read-only-session-listing.md).
  The browser opens from `/sessions` and from the startup picker a bare
  `-resume` requests, which lists through this same operation and pins the
  chosen session through the `/resume <uuid>` transition. See
  [`docs/adr/021-widen-resume-to-an-optional-value-that-opens-the-session-picker.md`](docs/adr/021-widen-resume-to-an-optional-value-that-opens-the-session-picker.md).
- **Pure rendering.** `internal/ui` is side-effect-free and takes data in,
  strings out. The timeline is rendered through a per-item cache; width changes
  and `/clear` trigger a full re-render, while global tool-output mode changes
  re-render only tool items.
- **Sanitize at the render boundary.** `ui.RenderItem` and `ui.StatusLine`
  strip terminal control sequences from stream content before styling it, so
  no timeline item kind can write escapes to the terminal and width math stays
  honest. Assistant markdown is scrubbed twice on purpose: glamour unescapes
  HTML character references while rendering (`&#7;`, `&#27;[2J`), so the
  decoded output is scrubbed again before it leaves the boundary — Ascii keeps
  no sequences, and a color profile keeps only the reviewed, rendition-only SGR
  that carries the theme's styling. After that scrub, `ui.RenderMarkdown` adds
  generated OSC 8 hyperlinks around visible `http(s)` URLs, one per wrapped
  line segment, so a link glamour hard-wraps remains openable; the link text is
  always the URL's own rendered characters and stream content still cannot
  supply an escape. Sanitization works on a copy; `internal/app` and the debug
  log keep the raw bytes. See
  [`docs/adr/005-untrusted-stream-content-is-sanitized-at-the-ui-render-boundary.md`](docs/adr/005-untrusted-stream-content-is-sanitized-at-the-ui-render-boundary.md)
  and
  [`docs/adr/022-generate-osc-8-hyperlinks-for-wrapped-assistant-urls.md`](docs/adr/022-generate-osc-8-hyperlinks-for-wrapped-assistant-urls.md).
- **Session state is a pure state machine.** `sessionState` (in `session.go`)
  decides the next run mode with no I/O, which is what makes the
  hijack-prevention behavior testable. Once a run announces a session id it
  pins future prompts to `--resume <session-id>`, whether the run succeeded,
  failed, was canceled, or ended before a completion record. No-session-ID
  completion leaves the current mode unchanged; the REPL has no latest-session
  fallback. See
  [`docs/guardrails/session-and-security.md`](docs/guardrails/session-and-security.md).
- **Config is startup-only.** Config files set stable REPL defaults before the
  TUI starts, using hardcoded defaults < XDG config < project-local config <
  CLI flags. The automatic project-local layer cannot set `cake-bin`: that key
  is ignored with a startup warning, so executable selection comes only from
  XDG config, explicit `-config`, or `-cake-bin`. Session-specific values stay
  outside the persisted config shape. See
  [`ADR 002`](docs/adr/002-config-file-for-repl-defaults.md) and its partial
  supersession in [`ADR 006`](docs/adr/006-project-local-config-cannot-select-the-cake-executable.md).
- **One cake process at a time.** Submitting while a task runs is rejected; the
  model tracks a single live `*cake.Run`. The external compactor is separate but
  serialized with it: it starts only after a completed turn, and while it runs
  the model refuses prompts and session-target changes until the child has
  exited and been reaped. See
  [`docs/adr/023-compact-context-between-turns-through-an-external-compactor.md`](docs/adr/023-compact-context-between-turns-through-an-external-compactor.md).
- **Prompt suggestions are opt-in, post-turn, and never gate input.** After a
  completed turn that pins a session, one model-owned `cake-suggest` request may
  run through `internal/suggest`, concurrently with an automatic compaction. It
  is read-only, bounded by a context timeout, and canceled by any composer use,
  a new session, a retarget, or quit. Its model/profile resolve from
  `[suggestions]` first and then the turn selection; reasoning effort comes
  from `[suggestions]` when set. A result is applied only
  while the request identity, session pin, idle state, and empty composer all
  still match; one that arrives while compaction gates the composer is held and
  applied after the compactor settles. It appears as dim placeholder ghost text
  rather than editable input. `Ctrl+S` submits the ghost as an ordinary prompt;
  slash-command parsing is bypassed so model output cannot execute a command.
  `-no-session` disables the feature, and the untrusted project-local config
  layer can only disable it. See
  [`docs/adr/028-offer-after-turn-prompt-suggestions-from-an-external-suggester.md`](docs/adr/028-offer-after-turn-prompt-suggestions-from-an-external-suggester.md),
  [`docs/adr/030-let-prompt-suggestions-select-their-own-model-and-profile.md`](docs/adr/030-let-prompt-suggestions-select-their-own-model-and-profile.md),
  [`docs/adr/031-run-after-turn-suggestions-alongside-compaction.md`](docs/adr/031-run-after-turn-suggestions-alongside-compaction.md),
  and
  [`docs/adr/032-let-prompt-suggestions-select-reasoning-effort.md`](docs/adr/032-let-prompt-suggestions-select-reasoning-effort.md).
- **Compaction is opt-in, between-turns, and in place.** A completed turn whose
  newest complete `turn_usage` figure reaches the configured trigger starts one
  model-owned `cake-compact` subprocess through `internal/compact`. Absolute
  mode uses `max-context-tokens` as a quality ceiling and `trigger-tokens` as
  the line; the effective trigger is capped at 75% of the model window when the
  window is below the ceiling, and only that trigger (never the ceiling) is
  passed as `-threshold-tokens`. Without the absolute fields the legacy percent
  fallback keeps its five-point headroom and passes `-threshold-percent`.
  Exactly one threshold mode reaches the compactor. While it runs, the timeline
  shows one start notice, the status line and composer carry its elapsed
  seconds, and the composer names Ctrl+C; the start notice is resolved into one
  outcome row when the child is reaped. The compactor re-checks the session and
  decides; a `compacted:false` result resolves the notice with an explicit skip
  row. Only a document that says `compacted:true` and `in_place:true` with the
  pinned session id is applied: the REPL keeps `ResumeID`, appends one shared
  marker, and seeds the status context from `after_tokens` as an estimate. A
  failure, cancellation, or other result keeps the session and figure and
  warns. The REPL never reads session files; `-no-session` disables the
  feature; and the
  untrusted project-local config layer cannot enable it, choose its executable,
  or tune any threshold, including the absolute fields. See
  [`docs/adr/023-compact-context-between-turns-through-an-external-compactor.md`](docs/adr/023-compact-context-between-turns-through-an-external-compactor.md),
  [`docs/adr/024-ignore-project-local-compaction-tuning.md`](docs/adr/024-ignore-project-local-compaction-tuning.md),
  and
  [`docs/adr/025-add-an-absolute-context-trigger-with-a-separate-quality-ceiling.md`](docs/adr/025-add-an-absolute-context-trigger-with-a-separate-quality-ceiling.md).
- **Child processes are detached from the REPL terminal.** On POSIX, every live
  prompt, replay, session listing, opt-in compaction, and prompt suggestion
  starts in its own session with `SysProcAttr{Setsid: true}` (one helper in
  `internal/childproc`), so the child has no controlling terminal and
  `/dev/tty` fails with `ENXIO`. Windows is a no-op and claims no equivalent
  boundary. Start failure is fail-closed, and tools that need a terminal now
  fail rather than prompt over the REPL. See
  [`ADR 027`](docs/adr/027-isolate-engine-subprocesses-from-the-repl-controlling-terminal.md).
- **Graceful cancellation.** Cancel sends SIGTERM to the child leader. A
  REPL-owned timer fires at the grace deadline and sends SIGKILL to the detached
  process group unless `cmd.Wait` has already reaped it; `cmd.WaitDelay` is one
  second later as a redundant PID-only fallback. Cancellation is classified
  from whether `cmd.Cancel` successfully signaled the process and, on POSIX,
  whether the process was signal-terminated—not from `ctx.Err()`—so a late
  Ctrl+C cannot relabel a finished run.
- **The terminal is handed back before the process stops.** A catchable external
  SIGTSTP is handled in `cmd/cake-repl`: the terminal is released, each tracked
  live or replay engine group is stopped with SIGSTOP, and the REPL's own group
  is stopped; a run that registers during the stop is paused as it registers.
  After SIGCONT the engine groups are continued before the modes
  Bubble Tea's `RestoreTerminal` leaves off are written and the terminal is
  re-acquired. The read-only listing is not tracked. Nothing in the model
  changes, so the conversation survives, and the engine is paused rather than
  killed. Inline mode parks the suspension on the alternate screen. See
  [`docs/adr/018-recover-terminal-ownership-around-an-external-suspend.md`](docs/adr/018-recover-terminal-ownership-around-an-external-suspend.md)
  and [`docs/guardrails/session-and-security.md`](docs/guardrails/session-and-security.md).

## Related docs

- [`README.md`](README.md) — user-facing behavior and flags.
- [`CONTRIBUTING.md`](CONTRIBUTING.md) — setup, commands, style, verification.
- [`AGENTS.md`](AGENTS.md) — agent routing.
- [`docs/guardrails/`](docs/guardrails/) — agent-facing rules by risk surface.
