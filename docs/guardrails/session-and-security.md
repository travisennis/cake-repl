# Guardrail: sessions, security, and subprocess lifecycle

**Scope.** Read before changing the run-mode state machine
(`internal/app/session.go`), subprocess start/cancel (`internal/cake/runner.go`),
or anything touching `-debug-log` or what is written to disk/terminal.

## Compatibility surfaces

- **Session pinning (security boundary).** After a successful task,
  `sessionState` pins future prompts to `--resume <session-id>`. This prevents
  another cake process that creates a newer session in the same cwd from
  **hijacking** the conversation. Fallback is `--continue` only when no session
  id was ever reported. A failed task must not advance the run mode. A canceled
  task preserves the pin to a session id that was already announced, so the
  next submission does not accidentally start a fresh session.
- **Run-mode transitions.** `RunFresh` → (success) → `RunResume`; `/new` resets
  to fresh; `Ctrl+N` resets to fresh, clears the timeline, and cancels and
  drains an active run without letting its late events or cancellation restore
  the old session pin; `/continue` and `/resume` set the next mode explicitly.
  **Active-run restriction:** `/new`, `/continue`, and `/resume` are rejected
  while a task is running with a warning to finish or cancel first. Only
  `/session`, `/help`, `/clear`, and `/exit` remain available during a run.
  Keep session transitions pure and I/O-free so they stay testable.
- **Secret handling.** Raw stream lines may contain prompts, tool output, and
  secrets. They go **only** to `-debug-log` (opened `0o600`), never to the
  timeline or stdout. Do not add logging that leaks raw stream content elsewhere.
- **Process lifecycle.** One cake process at a time. Cancel = SIGTERM then
  SIGKILL after `WaitDelay` (kill outright on Windows). stderr retained as a
  bounded tail for error display.

## Required checks / test focus

- `just test` for `session_test.go` (cover every transition, especially
  success-pins-resume and failure-does-not-advance).
- `just test-race` for any `runner.go` change.
- `just vuln` (govulncheck) when changing dependencies that touch process or I/O.

## Common failure modes

- **Reintroducing the hijack.** Falling back to `--continue` after a success
  that _did_ report a session id, or advancing run mode on a failed task.
- **Leaking secrets.** Sending raw stream lines to the timeline, stdout, or a
  world-readable file; widening `-debug-log` permissions.
- **Cancellation races.** Treating a finished run as canceled because Ctrl+C
  arrived late — classify from the `cmd.Cancel` flag plus signal-terminated
  process status on POSIX, not the context. See `runner.go` for how the atomic
  flag preserves ordering.
- **Zombie / runaway processes.** Removing the SIGKILL fallback or `WaitDelay`,
  or allowing more than one concurrent run.

## Related docs

- [`../../ARCHITECTURE.md`](../../ARCHITECTURE.md) — invariants.
- [`cake-integration-and-stream-json.md`](cake-integration-and-stream-json.md) — flags and stream.
- [`cli-and-user-output.md`](cli-and-user-output.md) — `-debug-log` flag, session commands.
