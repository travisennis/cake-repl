# Guardrail: cake integration and stream-json

**Scope.** Read before changing anything in `internal/cake/` (events, parser,
runner), the cake CLI arguments built in `Options.Args`, or how `internal/app`
consumes cake events. This is the project's core external contract.

## Compatibility surfaces

- **cake invocation.** Live prompts run as `cake --output-format stream-json`
  with optional `--resume <uuid>`, `--no-session`, `--model` / `--profile`,
  optional repeated `--add-dir <dir>` and `--toolbox <dir>`, and a
  `--sandbox <policy>` that defaults to `workspace-write-interactive` (ADR 017);
  `--tools <names>` / `--no-tools`, `--no-skills` /
  `--skills <names>`, and `--system-prompt <path>`. An initial fresh prompt
  may additionally use `--fork [<uuid>]`; once cake reports the forked session
  ID, later prompts use only its pinned `--resume <uuid>`. Read-only startup
  resume hydration runs the separate `cake --output-format stream-json replay
  <uuid>` command with no prompt. The other permitted invocation is the
  read-only, informational `cake sessions list --json`, run with the same
  working directory as a live prompt to list that directory's sessions. `--`
  must stay so a prompt beginning with `-` is never parsed as a flag.
- **stream-json schema.** The typed events in `events.go` (`task_start`,
  `session_meta`, `prompt_context`, `message`, `reasoning`, `function_call`,
  `function_call_output`, `hook_event`, `skill_activated`, `task_complete` +
  `usage`, `turn_usage` + `usage`, `compaction`, and `replay_error`) mirror
  cake's wire format.
  Field names and JSON tags are the contract; do not rename or repurpose them to
  match cake's output. `session_meta` carries `working_directory` and the
  optional model identity (`model` is the provider model ID, `model_config` the
  `[[models]]` entry name); `task_start` gains the same optional pair with
  cake#664, and `task_complete` decodes it ahead of any cake that reports it
  there. These fields feed the status-line model under
  [ADR 014](../adr/014-source-the-status-line-model-from-the-cake-stream.md).
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
  REPL runs. `cake-compact compact -json -threshold` is invoked with
  `-threshold-percent`, `-keep-tokens`, `-cake-bin` when cake-repl has one, an
  optional `-summarizer-model` from the configured model, and `-- <session-id>`.
  Never pass `-fork`: the integration is in place only. The one JSON document is
  decoded field by field (`Document`) with unknown fields ignored; `in_place` is
  read rather than assumed. Exit 0 with a document is success or a silent no-op
  (`compacted:false`); a nonzero exit is a failure whose bounded stderr tail is
  shown; cancellation is a warning. The process is the same subprocess discipline
  as a cake run: model-owned context, SIGTERM then `WaitDelay`, bounded
  stdout/stderr, no session-file reads by the REPL, and a content-free debug log
  line. The child inherits the REPL's environment and working directory, so
  `CAKE_DATA_DIR` and a relative `-cake-bin` select the same root and executable
  they do for cake; the REPL does not reimplement Cake's data-directory layout.
  See [ADR 023](../adr/023-compact-context-between-turns-through-an-external-compactor.md).
- **Engine isolation.** No reading cake session files, no parsing cake's
  human-readable output, no importing cake internals. The CLI (NDJSON streams
  and the versioned `sessions list --json` envelope) is the only contract; it is
  invoked only from `internal/cake`. Replay failures are structured
  `replay_error` records and non-zero exits: input errors use exit 3; corrupt,
  unsupported-format, and permission errors use exit 1. Older cake binaries that
  do not support replay must degrade to a non-fatal warning.
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
  inherited stdout pipe, cannot hang the REPL. See
  [ADR 016](../adr/016-allow-read-only-cake-sessions-list-for-the-session-browser.md)
  and
  [ADR 019](../adr/019-cancel-and-bound-the-read-only-session-listing.md).

## Required checks / test focus

- `just test` for `internal/cake`; `just test-race` when touching `runner.go`
  (it is concurrent: a goroutine pumps `Events` and delivers one `Result`).
- Add table cases to `parser_test.go` for new/changed event types and to
  `runner_test.go` (fake-cake shell scripts) for new subprocess behavior,
  including replay success and structured failure. `replay_test.go` covers
  timeline hydration and continued prompt execution.
- For session listing, add fake-cake cases for a valid envelope, an empty
  `data.sessions`, malformed JSON, a non-zero exit, and an unknown
  `schema_version`; none may be fatal. Also cover cancellation, the bounded
  teardown when a descendant holds stdout (short `waitDelay`, finite fake helper
  with cleanup), and the 8 MiB stdout cap.
- For the external compactor, add fake `cake-compact` scripts for a success
  document, a `compacted:false` no-op, a non-zero exit with stderr, malformed
  output, a missing binary, cancellation, and the `WaitDelay` teardown when a
  descendant holds stdout. App tests drive the trigger, the gate, the in-place
  success, the non-in-place rejection, and cancellation through `Ctrl+C` and
  `Ctrl+N`; none may need a real cake or a model call.
- Verify a real round trip if you have a `cake` binary, but never make a real
  cake binary a test requirement.

## Common failure modes

- **Breaking forward compatibility.** Unknown event types must decode to
  `Unknown` and unknown fields must be ignored — do not turn an unrecognized
  `type` into a hard error. A newer cake must never break the stream.
- **Dropping malformed lines silently.** Invalid JSON lines become synthetic
  `ParseError` events; the reader keeps going. Don't abort the stream on one bad
  line, and don't use `bufio.Scanner` (long tool-output lines overflow it).
- **Mislabeling cancellation.** Cancellation is derived from the `cmd.Cancel`
  flag plus signal-terminated process status on POSIX, not from `ctx.Err()`, so
  a late Ctrl+C cannot relabel a finished run. Preserve that ordering.
- **Unbounded memory.** stderr is kept as a bounded tail (`tailBuffer`); tool
  output is truncated for display and retained with the existing per-result
  ceiling. Replay uses the same limits; don't accumulate full streams in memory.
- **Mistaking a green suite for forward compatibility.** Every fixture under
  `internal/cake/testdata/fixtures/` is ours, so `just test` only proves we
  still decode the events we already know about. A record type cake added last
  week decodes to `Unknown` by design and fails nothing. A passing suite is not
  evidence about a newer cake: when a change touches decoding, say in the
  handoff whether a real-cake round trip was run or the coverage is unknown.
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
  stream's `turn_usage`; the compactor re-checks the session. A REPL-side session
  reader would cross the engine boundary and duplicate Cake's projection rules.

## Related docs

- [`../../ARCHITECTURE.md`](../../ARCHITECTURE.md) — system boundary and invariants.
- [`session-and-security.md`](session-and-security.md) — run-mode + secrets.
- [`cli-and-user-output.md`](cli-and-user-output.md) — how events render.
- [`../adr/011-replay-resumed-sessions-through-cake-stream-json.md`](../adr/011-replay-resumed-sessions-through-cake-stream-json.md) — replay decision.
