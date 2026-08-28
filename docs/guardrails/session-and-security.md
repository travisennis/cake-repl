# Guardrail: sessions, security, and subprocess lifecycle

**Scope.** Read before changing the run-mode state machine
(`internal/app/session.go`), subprocess start/cancel (`internal/cake/runner.go`),
or anything touching `-debug-log` or what is written to disk/terminal.

## Compatibility surfaces

- **Session pinning (security boundary).** Once a session id has been
  announced, `sessionState` pins future prompts to `--resume <session-id>`.
  This prevents another cake process that creates a newer session in the same
  cwd from **hijacking** the conversation. The pin applies on success, failure,
  and cancellation alike: cake writes a resumable session file in every case,
  so the next submission must not accidentally start a fresh session and orphan
  the work. Fallback to `--continue` is reserved for a **successful** task that
  reported no session id; a failed task with no session id leaves the run mode
  untouched rather than selecting `--continue`.
- **Session hydration.** Startup `-resume <uuid>` launches the read-only cake
  replay command before accepting a new prompt. Ordered replay events hydrate
  the local timeline; metadata restores known session/task ids, and replayed
  user messages are rendered because no prompt was submitted by the REPL.
  Hydration is not a live task, so the running state remains idle. A replay
  failure or unsupported cake binary produces a warning and leaves the explicit
  `--resume <uuid>` pin unchanged so the user can continue.
- **Run-mode transitions.** `RunFresh` → (any completion reporting a session
  id) → `RunResume`; `/new` resets to fresh; `Ctrl+N` resets to fresh, clears the timeline, and cancels and
  drains an active run without letting its late events or cancellation restore
  the old session pin; `/continue` and `/resume` set the next mode explicitly.
  **Active-run restriction:** `/new`, `/continue`, and `/resume` are rejected
  while a task is running with a warning to finish or cancel first. Only
  `/session`, `/help`, `/clear`, and `/exit` remain available during a run.
  Keep session transitions pure and I/O-free so they stay testable.
- **Secret handling.** Raw stream lines may contain prompts, tool output, and
  secrets. They go **only** to `-debug-log` (opened `0o600`; the mode applies
  at creation — an existing file's permissions are not narrowed), never to the
  timeline or stdout. Do not add logging that leaks raw stream content
  elsewhere. The one exception is an explicit user action: `Ctrl+Y` copies the
  raw (unsanitized) markdown of the most recent assistant response to the
  system clipboard on request, and never automatically. Pasting it into a
  terminal can reintroduce escaped content, so the user owns that risk.
- **Terminal-injection defense (security boundary).** Stream content is
  attacker-influenceable: tool output is the stdout of arbitrary commands and
  the contents of arbitrary files. `ui.Sanitize` strips ANSI escape sequences,
  expands tabs, drops the remaining C0/C1 controls and DEL, and replaces
  invalid UTF-8. It is applied unconditionally at the render boundary
  (`ui.RenderItem`, `ui.StatusLine`) so `CSI 2J`, `OSC 52`, and `OSC 8` cannot
  reach the terminal from any item kind. Newline is the only preserved control
  character. See
  [ADR 005](../adr/005-untrusted-stream-content-is-sanitized-at-the-ui-render-boundary.md).
- **Process lifecycle.** One cake process at a time, including while replay
  hydration is active. Cancel = SIGTERM then SIGKILL after `WaitDelay` (kill
  outright on Windows). stderr retained as a bounded tail for error display.
- **Replay isolation.** Replay is read-only and must not alter the session run
  mode or create a new session. Its late events are discarded after `Ctrl+N`,
  and a canceled replay is drained before another cake process starts.

## Required checks / test focus

- `just test` for `session_test.go` (cover every transition, especially
  success-pins-resume, failure-pins-resume, and failure-without-a-session-id-
  does-not-advance).
- `just test-race` for any `runner.go` change.
- `just vuln` (govulncheck) when changing dependencies that touch process or I/O.

## Common failure modes

- **Reintroducing the hijack.** Falling back to `--continue` after a completion
  that _did_ report a session id, or selecting `--continue` on a failed task.
- **Orphaning a session.** Leaving the run mode at `RunFresh` after a failure
  that reported a session id, so the user's next prompt silently starts a new
  session and abandons everything the failed run wrote.
- **Leaking secrets.** Sending raw stream lines to the timeline, stdout, or a
  world-readable file; widening `-debug-log` permissions.
- **Reopening terminal injection.** Adding a render path that bypasses
  `ui.RenderItem`/`ui.StatusLine`, or an opt-out that lets stream escapes
  through. Relying on glamour to discard escapes is not a defense.
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
