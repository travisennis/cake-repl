# Guardrail: sessions, security, and subprocess lifecycle

**Scope.** Read before changing the run-mode state machine
(`internal/app/session.go`), subprocess start/cancel (`internal/cake/runner.go`),
or anything touching `-debug-log` or what is written to disk/terminal.

## Compatibility surfaces

- **Executable selection.** Automatically loaded project-local `.cake-repl.toml`
  cannot set `cake-bin`; ignore the key and warn at startup using plain text
  that names the file and key without echoing the executable value. XDG config,
  explicit `-config` (including the project file), and `-cake-bin` remain
  allowed sources. See
  [ADR 006](../adr/006-project-local-config-cannot-select-the-cake-executable.md).

- **Sandbox policy default.** Live invocations pass
  `--sandbox workspace-write-interactive` unless the user overrides `-sandbox`.
  This is a deliberate escalation over cake's own `workspace-write` default
  because the REPL is interactive: `workspace-write-interactive` adds the macOS
  capabilities that let a command launch and automate other applications. An
  explicit empty `-sandbox ""` omits the flag, letting cake resolve its own
  policy from `CAKE_SANDBOX` or its settings. See
  [ADR 017](../adr/017-default-the-cake-sandbox-to-workspace-write-interactive.md).

- **Session pinning (security boundary).** Once a session id has been
  announced, `sessionState` pins future prompts to `--resume <session-id>`.
  This prevents another cake process that creates a newer session in the same
  cwd from **hijacking** the conversation. The pin applies on success, failure,
  and cancellation alike: cake writes a resumable session file in every case,
  so the next submission must not accidentally start a fresh session and orphan
  the work. If a task reports no session id, the current run mode remains
  unchanged; the REPL never selects an unrelated latest session.
- **Session hydration.** Startup `-resume <uuid>` launches the read-only cake
  replay command before accepting a new prompt. Ordered replay events hydrate
  the local timeline; metadata restores known session/task ids, and replayed
  user messages are rendered because no prompt was submitted by the REPL.
  Hydration is not a live task, so the running state remains idle. A replay
  failure or unsupported cake binary produces a warning and leaves the explicit
  `--resume <uuid>` pin unchanged so the user can continue.
- **Run-mode transitions.** `RunFresh` → (a run that announced a session id,
  at `task_start` or completion, whatever its outcome) → `RunResume`; `/new`
  resets to fresh; `Ctrl+N` resets to fresh, clears the timeline, and cancels and
  drains an active run without letting its late events or cancellation restore
  the old session pin; `/resume` sets the next mode explicitly. An initial
  `--fork` is consumed by its first fresh prompt, then the resulting session
  is pinned with `--resume`. **Active-run restriction:** `/new`, `/resume`, and
  `/sessions` are rejected while a task is running with a warning to finish or
  cancel first. Only `/session`, `/help`, `/clear`, and `/exit` remain available
  during a run.
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
  character. There is no implicit opt-out: the only exception is the explicit
  `-tool-color` flag, which lets `ui.SanitizeToolOutput` keep reviewed SGR
  (graphics-only) sequences in tool output blocks and nothing else, and is
  forced off under the Ascii profile. See
  [ADR 005](../adr/005-untrusted-stream-content-is-sanitized-at-the-ui-render-boundary.md)
  and [ADR 009](../adr/009-opt-in-sgr-passthrough-for-tool-output.md).
- **Assistant markdown is decoded after sanitization (security boundary).**
  Glamour unescapes HTML character references while rendering, so numeric
  references such as `&#7;`, `&#13;`, or `&#27;[2J` introduce C0 controls and
  whole escape sequences after the render boundary's `ui.Sanitize` pass. The
  rendered markdown is therefore scrubbed again before it can leave
  `ui.RenderMarkdown`: the Ascii profile keeps no sequences at all, and a color
  profile keeps only the reviewed, rendition-only SGR that carries the theme's
  styling, through the same policy as `-tool-color`. Never rewrite the markdown
  source to close this hole — glamour does not decode inside fenced code blocks
  or link destinations, so a source pass would corrupt code samples. See
  [ADR 005](../adr/005-untrusted-stream-content-is-sanitized-at-the-ui-render-boundary.md).
- **Process lifecycle.** One cake process at a time, including while replay
  hydration is active. Cancel = SIGTERM then SIGKILL after `WaitDelay` (kill
  outright on Windows). stderr retained as a bounded tail for error display.
- **Terminal ownership across a suspend (security boundary).** The REPL owns raw
  input, the cursor, mouse reporting, and (by default) the alternate screen, so
  it must never stop or exit without giving them back. An external, catchable
  SIGTSTP is handled in `cmd/cake-repl/suspend.go`: the terminal is released
  (`ReleaseTerminal`) before the process stops, the whole process group is
  stopped with SIGSTOP (SIGTSTP cannot be re-raised once `signal.Notify` has
  taken it; a group stop pauses the cake child with the REPL instead of leaving
  it running unwatched), and after SIGCONT the modes Bubble Tea's
  `RestoreTerminal` leaves off — mouse reporting — are written while the
  renderer is still stopped, then the terminal is re-acquired. A release that
  fails abandons the cycle and leaves the REPL running rather than stopping with
  raw mode still enabled. Inline mode parks the suspension on the alternate
  screen so the shell's own output cannot land in the live region. Uncatchable
  SIGSTOP and a REPL killed while stopped are best-effort and documented, never
  implied. See
  [ADR 018](../adr/018-recover-terminal-ownership-around-an-external-suspend.md).
- **Replay isolation.** Replay is read-only and must not alter the session run
  mode or create a new session. Its late events are discarded after `Ctrl+N`,
  and a canceled replay is drained before another cake process starts.

## Required checks / test focus

- `just test` for `session_test.go` (cover every transition, especially
  success-pins-resume, failure-pins-resume, and failure-without-a-session-id-
  does-not-advance) and `cmd/cake-repl` for the suspend cycle's step order and
  its safe failure on an unreleasable terminal.
- `just test-race` for any `runner.go` change.
- `just vuln` (govulncheck) when changing dependencies that touch process or I/O.
- Terminal behavior around a suspend has no unit-test surface beyond the step
  order and mode sequences, so a change there needs the driven session in
  [cli-and-user-output.md](cli-and-user-output.md), in both render modes.

## Common failure modes

- **Reintroducing the hijack.** Selecting an unrelated latest session after a
  completion that did not report a session id, instead of leaving the current
  run mode unchanged or requiring an explicit `/resume`.
- **Orphaning a session.** Leaving the run mode at `RunFresh` after a failure
  that reported a session id, so the user's next prompt silently starts a new
  session and abandons everything the failed run wrote.
- **Leaking secrets.** Sending raw stream lines to the timeline, stdout, or a
  world-readable file; widening `-debug-log` permissions.
- **Reopening terminal injection.** Adding a render path that bypasses
  `ui.RenderItem`/`ui.StatusLine`, or an implicit opt-out that lets stream
  escapes through. `-tool-color` is the one reviewed exception, and only for
  SGR in tool output; widen it to other item kinds or other escape families and
  it becomes this failure mode. Relying on glamour to discard escapes is not a
  defense.
- **Trusting the input pass to cover decoded markdown.** `ui.Sanitize` runs on
  the markdown source, but glamour unescapes character references while
  rendering, so the rendered output needs its own scrub; removing that scrub
  (or replacing it with `ansi.Strip`, which leaves bare C0 bytes) reopens the
  hole.
- **Cancellation races.** Treating a finished run as canceled because Ctrl+C
  arrived late — classify from the `cmd.Cancel` flag plus signal-terminated
  process status on POSIX, not the context. See `runner.go` for how the atomic
  flag preserves ordering.
- **Zombie / runaway processes.** Removing the SIGKILL fallback or `WaitDelay`,
  or allowing more than one concurrent run.
- **Stopping or exiting with the terminal still owned.** Stopping before
  releasing raw mode, the cursor, mouse reporting, or the alternate screen;
  assuming `RestoreTerminal` restores every mode (it restores bracketed paste
  and focus reporting, but not mouse reporting); stopping with SIGTSTP after the
  runtime has taken it, which is swallowed; killing the engine instead of pausing
  it with the process group; or letting a shell's job-control output land in the
  live region because the suspension was not parked on the alternate screen in
  inline mode.

## Related docs

- [`../../ARCHITECTURE.md`](../../ARCHITECTURE.md) — invariants.
- [`cake-integration-and-stream-json.md`](cake-integration-and-stream-json.md) — flags and stream.
- [`cli-and-user-output.md`](cli-and-user-output.md) — `-debug-log` flag, session commands.
