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
  `usage`, and `replay_error`) mirror cake's wire format. Replay omits the
  session-only `turn_usage` record. Field names and JSON tags are the contract;
  do not rename or repurpose them to match cake's output. `session_meta`
  carries `working_directory` and the optional model identity (`model` is the
  provider model ID, `model_config` the `[[models]]` entry name); `task_start`
  gains the same optional pair with cake#664, and `task_complete` decodes it
  ahead of any cake that reports it there. These fields feed the status-line
  model under
  [ADR 014](../adr/014-source-the-status-line-model-from-the-cake-stream.md).
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
  degrades to an empty or warning state, never a fatal error. See
  [ADR 016](../adr/016-allow-read-only-cake-sessions-list-for-the-session-browser.md).

## Required checks / test focus

- `just test` for `internal/cake`; `just test-race` when touching `runner.go`
  (it is concurrent: a goroutine pumps `Events` and delivers one `Result`).
- Add table cases to `parser_test.go` for new/changed event types and to
  `runner_test.go` (fake-cake shell scripts) for new subprocess behavior,
  including replay success and structured failure. `replay_test.go` covers
  timeline hydration and continued prompt execution.
- For session listing, add fake-cake cases for a valid envelope, an empty
  `data.sessions`, malformed JSON, a non-zero exit, and an unknown
  `schema_version`; none may be fatal.
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

## Related docs

- [`../../ARCHITECTURE.md`](../../ARCHITECTURE.md) — system boundary and invariants.
- [`session-and-security.md`](session-and-security.md) — run-mode + secrets.
- [`cli-and-user-output.md`](cli-and-user-output.md) — how events render.
- [`../adr/011-replay-resumed-sessions-through-cake-stream-json.md`](../adr/011-replay-resumed-sessions-through-cake-stream-json.md) — replay decision.
