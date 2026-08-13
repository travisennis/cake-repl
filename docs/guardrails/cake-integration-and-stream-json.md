# Guardrail: cake integration and stream-json

**Scope.** Read before changing anything in `internal/cake/` (events, parser,
runner), the cake CLI arguments built in `Options.Args`, or how `internal/app`
consumes cake events. This is the project's core external contract.

## Compatibility surfaces

- **cake invocation.** cake is always run as `cake --output-format stream-json`
  with at most one of `--continue` / `--resume <uuid>`, optional `--model` /
  `--profile`, optional repeated `--add-dir <dir>` (read-only sandbox
  directories; cake resolves relative paths against its own cwd), and
  `-- <prompt>` last. `--` must stay so a prompt beginning with
  `-` is never parsed as a flag.
- **stream-json schema.** The typed events in `events.go` (`task_start`,
  `message`, `reasoning`, `function_call`, `function_call_output`, `hook_event`,
  `task_complete` + `usage`) mirror cake's wire format. Field names and JSON
  tags are the contract; do not rename or repurpose them to match cake's output.
- **Engine isolation.** No reading cake session files, no parsing cake's
  human-readable output, no importing cake internals. The CLI + NDJSON stream is
  the only contract. See [`../../ARCHITECTURE.md`](../../ARCHITECTURE.md).

## Required checks / test focus

- `just test` for `internal/cake`; `just test-race` when touching `runner.go`
  (it is concurrent: a goroutine pumps `Events` and delivers one `Result`).
- Add table cases to `parser_test.go` for new/changed event types and to
  `runner_test.go` (fake-cake shell scripts) for new subprocess behavior.
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
  output is truncated for display. Don't accumulate full streams in memory.

## Related docs

- [`../../ARCHITECTURE.md`](../../ARCHITECTURE.md) — system boundary and invariants.
- [`session-and-security.md`](session-and-security.md) — run-mode + secrets.
- [`cli-and-user-output.md`](cli-and-user-output.md) — how events render.
