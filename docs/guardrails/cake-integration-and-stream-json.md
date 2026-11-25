# Guardrail: cake integration and stream-json

**Scope.** Read before changing anything in `internal/cake/` (events, parser,
runner), the cake CLI arguments built in `Options.Args`, or how `internal/app`
consumes cake events. This is the project's core external contract.

## Compatibility surfaces

- **cake invocation.** Live prompts run as `cake --output-format stream-json`
  with optional `--resume <uuid>`, `--no-session`, `--model` / `--profile`,
  optional repeated `--add-dir <dir>` and `--toolbox <dir>`, and a
  `--sandbox <policy>` that defaults to `workspace-write-interactive`;
  `--tools <names>` / `--no-tools`, `--no-skills` /
  `--skills <names>`, and `--system-prompt <path>`. An initial fresh prompt
  may additionally use `--fork [<uuid>]`; once cake reports the forked session
  ID, later prompts use only its pinned `--resume <uuid>`. A resumed run that
  cake refuses with its exit-3 context-mismatch preflight is retried exactly
  once as `--fork <uuid>` with the same prompt; the fork is a fresh invocation,
  so it carries the fresh-session controls plus an explicit model and any known
  stored profile rather than the resume pass-through omissions. The retry
  never runs for a canceled run, `-no-session`, or a non-mismatch exit 3. When
  the target is a resume, cake-repl omits every pass-through flag that was not
  explicitly set; cake restores the session's stored `session_meta.run_config`
  and working directory. The hardcoded `-sandbox` default and config-derived model/profile
  are fresh-session defaults only. A session created in this process keeps its
  creation controls and replays them on a follow-up turn until a replay reports
  a stored `run_config`, so an older engine does not change sandbox or profile
  on turn two. Read-only startup
  resume hydration runs the separate `cake --output-format stream-json replay
  <uuid>` command with no prompt. The same command hydrates a `-fork` source at
  startup, and that hydration is presentation-only: it must not pin the source,
  move the run mode to resume, or adopt the source's id, task id, model,
  profile, stored run config, context usage, or working directory, so the first
  prompt remains a fresh `cake --fork [<uuid>]` invocation. A bare `-fork`
  resolves this working directory's newest session with the permitted
  `sessions list --json` invocation at startup and passes that exact UUID to
  `--fork`, so the hydrated history is the fork source. The other permitted
  invocations are the read-only, informational `cake sessions list --json`, run
  with the same working directory as a live prompt to list that directory's
  sessions, and the read-only `cake trust --status --json` startup preflight
  described below. `--` must stay so a prompt beginning with `-` is never
  parsed as a flag.
- **Context-mismatch fork fallback.** `Result.ContextMismatch` is the one
  place that reads cake's human text: exit code 3, no cancellation or process
  error, the stderr marker
  `Session context mismatch: context for session `, and no
  `--strict-context-drift`. Exit code 3 alone is not a trigger (it also covers
  invalid flags, models, and configuration), and the strict-drift message is
  excluded because it reports a user-requested check. If cake rewords the
  message the fallback does not run and the ordinary exit-3 error is shown.
  The fork source is the resume target of the failed invocation, and the retry
  records no second user timeline row and no second history entry.
- **stream-json schema.** The typed events in `events.go` (`task_start`,
  `session_meta`, `prompt_context`, `message`, `reasoning`, `function_call`,
  `function_call_output`, `hook_event`, `skill_activated`, `task_complete` +
  `usage`, `turn_usage` + `usage`, `compaction`, and `replay_error`) mirror
  cake's wire format.
  Field names and JSON tags are the contract; do not rename or repurpose them to
  match cake's output. `session_meta` carries `working_directory`, the optional
  model identity (`model` is the provider model ID, `model_config` the
  `[[models]]` entry name), and the optional stored `run_config`. A present
  `run_config` makes restore support authoritative; an absent profile inside it
  is a known empty selection, not a fallback to config. `task_start` gains the
  same optional model pair with cake#664, and `task_complete` decodes it ahead
  of any cake that reports it there. The status line uses the reported identity
  as soon as `task_start` arrives, including during a fresh turn.
- **Per-turn usage (`turn_usage`).** Live and replayed streams emit a
  `turn_usage` record for every main-agent provider attempt that reports usage;
  summary and compaction attempts are not streamed as context measurements. It
  carries `session_id`, `task_id`, `turn`, `usage` (`input_tokens`,
  `output_tokens`, `total_tokens`, plus optional `input_tokens_details` and
  `output_tokens_details`), optional `attempt`/`terminal_class` on retried or
  failed attempts, optional `usage_presence`, `model`, `response_model`,
  `provider_request_id`, `timestamp`, and optional `context_window` /
  `remaining_context_tokens` when the model configures a window and the usage
  is complete. A first successful attempt omits `attempt` and
  `terminal_class`. `task_complete.usage` stays the task aggregate and must
  never be read as the current context. For the status line, only a complete
  record is a measurement: `usage_presence` must be absent (a legacy complete
  record) or `complete`, a record with no positive input count is ignored, and
  the newest complete record wins, including its window. The app clears the
  measurement whenever the session target changes or resets; it never reads
  cake session files for the figure.
- **Context compaction (`compaction`).** Replay emits one `compaction` record
  at its physical position when a session has a context checkpoint. The typed
  event decodes the record's identity and metadata, including the two fields
  the marker and estimate read: the `retained` count and `estimated_input`.
  `trigger`, `summary_instruction_version`, a bounded first line of `summary`,
  and `model` are decoded for the event contract and are not rendered. The
  retained tail's messages, reasoning, and tool payloads are never retained on
  the event or rendered as timeline items, and one record never expands into
  duplicate transcript entries. The record decodes field by field (its wire
  names are documented on the type; it has no struct tags) so an unknown or
  malformed optional field keeps the record and the rest of its marker instead
  of breaking the stream. The app appends exactly one `KindInfo` marker such as
  `context compacted 118k -> 24k, 4 records retained`. Replay seeds the status
  context from `estimated_input.after_tokens`, and the live compaction result
  seeds the same `ContextUsage` figure. Either way it is visibly marked
  estimated, and a newer complete `turn_usage`, replayed after the checkpoint or
  reported by a live turn, replaces it. A live `compaction` stream record never
  seeds the estimate. See
  [cli-and-user-output.md](cli-and-user-output.md) for the rendered forms.
- **External compactor (`internal/compact`).** The only non-cake executable the
  REPL runs. `cake-compact compact -json -threshold` is invoked with exactly one
  threshold mode: `-threshold-tokens` carrying the effective absolute line in
  absolute mode, or `-threshold-percent` in the legacy fallback. It also gets
  `-keep-tokens`, `-cake-bin` when cake-repl has one, `-summarizer-model`,
  `-summarizer-profile`, and `-summarizer-reasoning-effort`, and
  `-- <session-id>`. The model and profile come from `[compaction] model` and
  `profile` when set and otherwise from the REPL's turn `-model`/`-profile`;
  each is omitted when unset. Reasoning effort comes from
  `[compaction] reasoning-effort` and is passed only when set, so cake's own
  default otherwise. All three selection keys are trusted-config only. The
  absolute quality ceiling is never passed as the trigger. The trigger is a
  between-turn target, not a live cap: the REPL evaluates it after a completed
  turn, so one invocation can grow past it before the compactor runs. Never
  pass `-fork` or `-force`: the
  integration is in place and threshold-gated only. The one JSON document is
  decoded field by field (`Document`) with unknown fields ignored; `in_place` is
  read rather than assumed. Exit 0 with a document is success or a no-op
  (`compacted:false`); the no-op still resolves the live start notice with a
  skip row in the timeline. A nonzero exit is a failure whose bounded stderr
  tail is shown; cancellation is a warning. The process is the same subprocess
  discipline as a cake run: model-owned context, SIGTERM then `WaitDelay`, bounded
  stdout/stderr, no session-file reads by the REPL, and a content-free debug log
  line. It also starts on the same POSIX session boundary as a cake run
  (`internal/childproc`), so neither it nor its descendants can reach the REPL's
  controlling terminal. The child inherits the REPL's environment and runs in
  the same `-cwd`, so `CAKE_DATA_DIR` selects the same root and a relative
  `-cake-bin` resolves in that directory; the REPL does not reimplement Cake's
  data-directory layout. A relative `CAKE_DATA_DIR` is resolved against the
  startup run directory before replay starts, so a later cwd restoration cannot
  make replay and the live turn read different stores.
- **External suggester (`internal/suggest`).** The opt-in prompt suggester is
  invoked only as
  `cake-suggest suggest -json [-cake-bin <path>] [-model <name>] [-profile <name>]
  [-reasoning-effort <effort>] -- <session-id>`. It is read-only. The
  `-model`/`-profile` values resolve from `[suggestions]` first and then
  independently from the turn selection; `-reasoning-effort` is passed only
  when `suggestions.reasoning-effort` is set. Exactly one JSON
  document is decoded field by field (`suggested`, `suggestion`, `session_id`,
  `reason`) with unknown fields ignored. Exit 0 with a document is the only
  success shape; `suggested:false` is a silent no-op. The REPL accepts the
  suggestion only when the document's `session_id` matches the request, the
  request identity is current, the session pin is unchanged, the REPL is idle,
  and the composer is empty. A shown ghost survives typing and returns when the
  composer is empty again; explicit draft or session actions discard it. A
  request starts alongside an automatic
  compaction; because Cake replay is read-only and skips a trailing partial
  record, the concurrent checkpoint append is safe. A result that arrives while
  compaction gates the composer is held and applied after the compactor
  settles. The text is sanitized, whitespace-collapsed to one line, and bounded
  before it becomes placeholder ghost text. `Ctrl+S` fills the composer with
  that text without submitting; a second `Ctrl+S` submits it. While the filled
  text is unchanged, submit bypasses slash-command parsing; editing it restores
  normal parsing. A clean startup `-resume <uuid>` hydration ending at a
  completed, non-errored turn starts one request after the replay is reaped.
  Cancellation is silent when the child is signal-terminated; a child that exits
  non-zero on its own after the cancel is a failure. A missing binary, non-zero
  exit, malformed document (including a missing required `suggested` field), or
  timeout warns at most once per REPL process. The process uses the same
  subprocess discipline as a cake run: model-owned context, SIGTERM then
  `WaitDelay`, bounded stdout/stderr, `internal/childproc` isolation, and a
  content-free debug-log line. Its model/profile resolve from `[suggestions]`
  first and then the effective turn selection: an explicit `/model`/`/profile`,
  or the replayed session value on resume. Quit and the unobserved-exit teardown
  drain an in-flight request, so its canceled child is reaped before the REPL
  returns.
  It never reads or writes a session file.
- **Engine isolation.** No reading cake session files, no parsing cake's
  human-readable output, no importing cake internals. The CLI (NDJSON streams
  and the versioned `sessions list --json` and `trust --status --json`
  envelopes) is the only contract; it is invoked only from `internal/cake`.
  Replay failures are structured `replay_error` records and non-zero exits:
  input errors use exit 3; corrupt, unsupported-format, and permission errors
  use exit 1. Older cake binaries that do not support replay must degrade to a
  non-fatal warning. The opt-in path watcher is front-end state only: it lists
  directory entries and starts an ordinary app turn; it never invokes cake,
  reads a cake session file, or consumes another tool's inbox.
- **Read-only session listing.** `cake sessions list --json` returns the
  envelope `{schema_version, command, status, summary, data.sessions[]}` where
  each session carries `session_id`, `first_prompt`, and `timestamp` (cake 0.1.0,
  `schema_version` 1). Decode it forward-compatibly: ignore unknown fields and a
  newer `schema_version`. It is informational only — list and select, never
  mutate — and any non-zero exit, malformed JSON, or unsupported envelope
  degrades to an empty or warning state, never a fatal error. The listing is a
  model-owned operation: closing or replacing the browser and quitting cancel it,
  a superseded result is dropped by request identity, and `ListSessions` bounds
  its teardown with `WaitDelay` so a stuck helper, or a descendant holding the
  inherited stdout pipe, cannot hang the REPL.
- **Project trust preflight.** The startup probe is one read-only
  `cake trust --status --json` invocation through `internal/cake`, with the
  configured `-cake-bin`, the same `-cwd` and environment as a live prompt, a
  1 MiB stdout cap, an 8 KiB stderr tail, group cancellation, a 3-second
  `WaitDelay`, and a 10-second context deadline. It never writes a trust record
  and never runs `cake trust`. Decode the diagnostic document's `data.state`,
  `project_root`, `authority[].path`, `authority[].keys`, `changes[]`, and
  `invalid_reason`; ignore unknown fields and a newer `schema_version`. A
  missing or empty `state` is malformed output. `trusted` and `not_required`
  are silent; `untrusted`, `changed`, `invalid`, and any unrecognized state
  append one advisory timeline warning when the probe returns, normally during
  startup before the first prompt. It names the
  state, authority or changes, canonical project root, and the `cake trust`
  command. The warning never gates input, so an immediate submission can arrive
  first. Any probe failure — non-zero exit,
  older cake without the subcommand, malformed JSON, or timeout — is
  debug-log-only and never a timeline item or startup failure. Skip the probe
  when `CAKE_TRUST_PROJECT=1` or `CAKE_PROJECT_CONFIG=off` is set because the
  live prompt does not depend on the project's persistent trust record;
  `trust --status` always inspects the project regardless of those variables.
  Each canonical project root,
  including a linked worktree, has its own record.

## Verification focus

- `just ci` for every change.
- The parser and runner encode cake's stream contract. Verify a change by
  driving the REPL against a real `cake` and, when the stream shape changed,
  exercising the affected commands by hand (`replay <uuid>`,
  `sessions list --json`, `trust --status --json`, an external `cake-compact`,
  or an external `cake-suggest`).
- There is no fixture harness. When a failure mode needs a fake engine, write a
  temporary script outside the repository and do not commit it.
- For the trust preflight, confirm startup stays silent on a probe failure and
  warns only for a successful document with an attention state.
- For the external compactor and suggester, confirm each failure class is
  contained and no request needs a model call unless you are explicitly testing
  a live call.
- When a change touches decoding, say in the handoff whether a real-cake round
  trip was run or the coverage is unknown.

## Common failure modes

- **Breaking forward compatibility.** Unknown event types must decode to
  `Unknown` and unknown fields must be ignored — do not turn an unrecognized
  `type` into a hard error. A newer cake must never break the stream.
- **Dropping malformed lines silently.** Invalid JSON lines become synthetic
  `ParseError` events; the reader keeps going. Don't abort the stream on one bad
  line, and don't use `bufio.Scanner` (long tool-output lines overflow it).
- **Forking on every exit 3.** Only the exit-3 context-mismatch marker may
  start the one-shot fork retry. A generic input error, a canceled run, a
  strict-drift failure, or `-no-session` must keep the ordinary error and pin.
  The retry itself is a fresh run, so it must never retry again; the resume
  operation key, not the fork source, gates eligibility.
- **Treating fork hydration as a resume.** `-fork [<uuid>]` replays the source
  session only to render it. Its `session_meta`, `task_start`, `task_complete`,
  `turn_usage`, and `compaction` records must not set the session pin, run
  mode, model, profile, stored run controls, context measurement, or working
  directory; the first prompt is still `--fork`, and no post-hydration
  suggestion may start because the forked session has no id yet. A bare `-fork`
  resolves its source once, with the read-only listing, and forks that id
  instead of leaving cake to choose a different latest session at prompt time.
- **Mislabeling cancellation.** Cancellation is derived by the shared
  `childproc.Classify` from whether `cmd.Cancel` successfully signaled the
  process (the run's cancel-requested phase) plus signal-terminated process
  status on POSIX, not from `ctx.Err`,
  so a late Ctrl+C cannot relabel a finished run. Preserve that ordering.
- **Making the trust preflight authoritative or fatal.** A probe failure, an
  older cake without `trust`, or malformed output must never block startup or
  prompt submission, and the probe must never write a trust record. Only a
  successful document with an attention state may add a warning.
- **Unbounded memory.** stderr is kept as a bounded tail
  (`childproc.TailBuffer`); tool output is truncated for display and retained
  with the existing per-result
  ceiling. Replay uses the same limits; don't accumulate full streams in memory.
- **Mistaking a successful decode for forward compatibility.** A record type
  cake added last week decodes to `Unknown` by design and fails nothing. A
  passing manual run is not evidence about a newer cake: when a change touches
  decoding, say in the handoff whether a real-cake round trip was run or the
  coverage is unknown.
- **Reading cumulative usage as context.** `task_complete.usage` sums the whole
  task, so its input count grows past any single context window. The status
  context segment reads only the newest complete `turn_usage` record's input
  tokens; never derive it from the completion aggregate.
- **Treating a compactor result as authoritative about the session id.** Accept
  a live compaction only when the document says `compacted:true` and
  `in_place:true` and its `session_id` is the pinned one. A minimal-fork result
  has a different id; retargeting to it silently abandons the session the user
  is in. The REPL keeps its pin and warns instead.
- **Reading session files to decide when to compact.** The trigger uses the
  stream's `turn_usage`; in absolute mode the line is `trigger-tokens`, capped at
  75% of the model window when the window is below the ceiling, and in percent
  mode it is five points below `threshold-percent`. The compactor re-checks the
  session. A REPL-side session reader would cross the engine boundary and
  duplicate Cake's projection rules.
- **Treating a suggestion as user input.** A suggestion is model output derived
  from session content. Submit it as an ordinary prompt, never through
  `ParseCommand`, or a transcript can execute a slash command. Apply it only
  when the composer is empty and the request/session identity still matches; a
  late result must never overwrite a draft.
- **Reading session files for a suggestion.** The suggester owns that read. The
  REPL passes the pinned session id and consumes one document; it never opens
  the session file or reconstructs the transcript from the timeline.

## Related docs

- [`../../ARCHITECTURE.md`](../../ARCHITECTURE.md) — system boundary and invariants.
- [`session-and-security.md`](session-and-security.md) — run-mode + secrets.
- [`cli-and-user-output.md`](cli-and-user-output.md) — how events render.
