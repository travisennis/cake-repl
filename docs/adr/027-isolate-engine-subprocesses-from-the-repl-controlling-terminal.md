---
status: accepted
date: 2026-10-26
decision-makers: Travis Ennis
---
# Isolate engine subprocesses from the REPL controlling terminal

## Context and Problem Statement

cake-repl owns its terminal. Bubble Tea puts input into raw mode, hides the
cursor, enables mouse reporting, and by default uses the alternate screen. A
prompt, a replay, and a session listing each spawn a `cake` child with stdout
and stderr captured as pipes. Go gives a child with no configured stdin the null
device, so the captured streams are not the exposure. The exposure is `/dev/tty`,
the kernel's name for the process's controlling terminal: a child that opens it
can change the terminal's modes, read keystrokes, or write over the REPL's
frames while every byte of its captured stdout stays clean.

That path was reproduced on 2026-10-08 (master `70677b2`, macOS, tmux, the
checked-in fake cake). A `-cake-bin` wrapper containing
`stty sane < /dev/tty` before `exec` left the pane in cooked, echoing mode: Up
arrived as the literal characters `^[[A`, key reports appeared over the status
line, and Ctrl+Z suspended the REPL. The wrapper's own output was captured
normally, so neither stream decoding nor render-boundary sanitization can see
the interference.

`internal/cake.Start` sets no `SysProcAttr`, so the child stays in the REPL's
session, process group, and controlling terminal. `internal/cake.ListSessions`
spawns the same way. A separate process group is not sufficient: on 2026-10-26
the following was observed on macOS 27.0.1 with Go 1.27.3 inside a tmux pty, and
the same kernel rule is specified for every POSIX system:

- An inherited child could read and write `/dev/tty`.
- A `Setpgid`-only child could read and write `/dev/tty`.
- A `Setsid` child got `ENXIO` ("Device not configured") for both.

Suspension is coupled to this topology. `cmd/cake-repl/suspend.go` releases the
terminal and then stops the REPL's whole process group with
`syscall.Kill(0, syscall.SIGSTOP)`, relying on cake being a member of that group.
If cake is detached, it no longer pauses with the REPL. ADR 018 decided that the
engine must not be left running unwatched, so detaching without coordination
would trade one defect for another.

The project ships Linux, macOS, and Windows binaries. Windows has no controlling
terminal or session concept; `CONIN$` and `CONOUT$` remain reachable regardless
of process attributes, so no equivalent guarantee is available there.

## Decision Drivers

- The terminal is a boundary: only the REPL and the user's shell may change its
  modes or consume its input.
- Keep the supported cake CLI contract, the pipe-based stream-json I/O, the
  working directory, and the session run modes unchanged.
- Preserve cake's own shutdown semantics: cake treats SIGTERM like Ctrl+C during
  a turn and terminates its tool children, so the REPL must not preempt that
  cleanup.
- Do not leave descendants of a hung engine process running after cancellation.
- Do not leave engine work running unwatched while the REPL is suspended.
- State platform behavior honestly; do not quietly fall back to an unisolated
  child, and do not imply a general sandbox.
- Verify with fake children and tests that do not require a real cake or a
  mandatory tmux dependency in CI.

## Considered Options

- **Keep the current topology.** Rejected: the reproduced defect is exactly this
  inheritance, and it silently defeats terminal ownership.
- **Start the child in its own process group with `Setpgid`.** Rejected: the
  probe shows `/dev/tty` is still readable and writable, so the guarantee would
  be cosmetic.
- **Start the child in a new session with `Setsid` and define its lifecycle.**
  Chosen: one attribute removes the controlling terminal on POSIX, and the
  lifecycle gaps it creates (descendant cleanup, suspend coordination) are
  explicit and testable.
- **Double-fork or daemonize the child.** Rejected: `Setsid` already removes the
  controlling terminal, and keeping the direct child is what makes cancellation
  and suspend signaling possible.
- **Allocate a pseudo-terminal per child.** Rejected: it hands the child a
  terminal, which is the thing to prevent, and it changes the stream framing.
- **Deny `/dev/tty` with Seatbelt or Landlock.** Rejected: platform-specific
  policy in the wrong layer, overlapping cake's own sandbox, and unavailable on
  Windows.

## Decision Outcome

Chosen option: every live, replay, and session-listing child the REPL spawns
for cake runs in a new session with no controlling terminal, and the lifecycle
consequences are handled explicitly. The opt-in compactor is a separate,
documented gap rather than part of this guarantee.

- **Mechanism.** `internal/cake` applies one process-attribute helper to the live
  run, replay, and session listing: `SysProcAttr{Setsid: true}` on `!windows`.
  Windows leaves the attribute unset. One helper, not an ad-hoc assignment at
  each spawn site, keeps the policy from drifting.
- **Cancellation.** SIGTERM still goes to the child first, so cake can finish
  writing session records and stop its tools. A REPL-owned timer fires at the
  accepted three-second grace deadline; if the run has not been reaped by then,
  the REPL sends SIGKILL to the child's process group. `exec`'s own `WaitDelay`
  kill is PID-only and can race the leader's reaping, so the command's
  `WaitDelay` is set one second later as a redundant PID-only fallback and the
  group kill is the primary bound. A run already marked reaped is not followed
  by a group signal, so a descendant that closed the inherited pipes (a dev
  server started by a tool) outlives the turn exactly as it does today. A
  descendant that still holds the inherited stdout or stderr pipe is killed with
  the group, because the REPL cannot finish reading the run until that pipe
  closes.
- **Suspension.** The suspend cycle keeps ADR 018's order and intent but adds
  explicit engine coordination: release the terminal, stop each tracked live or
  replay run's process group with SIGSTOP, stop the REPL's own group (unchanged),
  and after SIGCONT continue the engine groups before re-acquiring the terminal
  and repainting. A run that registers between the stop snapshot and the REPL's
  own stop is itself paused by the registry, so that window cannot leave engine
  work running unwatched. The read-only session listing is not tracked: it is
  bounded only after it exits or is canceled, so a stuck listing can continue
  through a suspend; it cannot touch the terminal, and closing or replacing the
  browser or quitting still cancels it. A REPL killed while stopped can still
  leave an engine stopped; that remains the documented ADR 018 limit.
- **Start failure is fail-closed.** If the child cannot be started with the
  attribute, starting the run fails and the error is shown. There is no silent
  unisolated retry.
- **Signals.** After `Setsid` the terminal no longer delivers Ctrl+C or SIGTSTP
  to cake; only the REPL receives them. Ctrl+C reaches cake as the REPL's
  SIGTERM, which cake handles like its own Ctrl+C, so cancellation behavior is
  unchanged. An external SIGTSTP (or a terminal whose ISIG is enabled) reaches
  cake as the explicit SIGSTOP from the suspend cycle; in the normal raw-mode
  REPL, Ctrl+Z is only a keystroke.
- **Tools that need a terminal now fail.** `sudo`, an SSH passphrase prompt,
  `gh auth login`, or an editor that opens `/dev/tty` receives `ENXIO` instead of
  prompting over the REPL. That is accepted: a prompt raised by a tool must be
  surfaced through cake's stream, and no opt-out flag is added in this change.
- **Partial supersession of ADR 018.** ADR 018 says the process group is stopped
  "so the cake child pauses with the REPL". The intent still holds; the mechanism
  no longer does. ADR 018 keeps its accepted status, and this ADR records the
  replacement of that one mechanism.

The explicit non-goals are a named terminal device (`/dev/ttysNNN`), signals from
another same-user process, a descendant that leaves the engine's process group or
session itself (`setpgid` or `setsid`), Windows console handles, and any claim of
a general sandbox. The opt-in `cake-compact` child in `internal/compact` is
outside this task's scope and keeps its current topology; it is a known gap
rather than an implied guarantee, tracked as task 124.

### Consequences

- Good: engine children and their descendants cannot change the REPL's terminal
  modes, read its input, or write over its frames through the controlling
  terminal. The reproduced `stty sane < /dev/tty` wrapper fails instead of
  corrupting the pane.
- Good: cancellation now reaps a hung child's whole process group, while a
  descendant that closed the inherited pipes still survives a graceful exit.
- Good: session identity is observable in tests without a terminal: a detached
  child's session id equals its own pid, while the REPL's does not.
- Bad: tools that require a terminal now fail rather than prompt.
- Bad: the compactor remains a second, unaddressed terminal path until it is
  brought under the same helper; task 124 tracks that follow-up.
- Bad: Windows gets no terminal-isolation guarantee; the existing topology is
  documented as the limit.
- Bad: a terminal hangup or an abrupt REPL death no longer sends the engine the
  group-directed termination it receives today, because it is outside the REPL's
  session. Normal quit, Ctrl+C, and SIGTERM paths still cancel it, but a crash
  can leave cake running until it finishes. No platform-specific parent-death
  cleanup is added in this change.
- Bad: the REPL signals a run's process group while the run is registered. A
  child reaped between the registry snapshot and the signal can have its pid
  reused by an unrelated group; that narrow race is accepted rather than closed
  with platform-specific pid handles.

## Amendment (2026-10-27)

Task 124 closed the gap this ADR recorded for the opt-in compactor. It applies
the same decision to one more child; it does not introduce a new boundary. The
decision outcome, non-goals, and consequences above are left as the record of
what was accepted; where they call the compactor a documented gap, this
amendment closes it.

- `internal/compact` calls the same helper before starting `cake-compact`, so the
  compactor and its descendants cannot open `/dev/tty` on POSIX. Windows remains
  a no-op that claims no isolation.
- The helper moved from `internal/cake/process_unix.go` into the leaf
  `internal/childproc` package so the engine and the compactor share it without
  either importing the other. `internal/cake` still applies it to the live run,
  replay, and session listing.
- The compactor's lifecycle is otherwise unchanged: cancellation still sends
  SIGTERM to the leader, `WaitDelay` still bounds teardown, and the threshold and
  result-document contract are untouched. A fake-compactor test proves the
  boundary with session identity (`getsid(pid) == pid`); a second test adds a
  `/dev/tty` probe that asserts only when the test process owns a controlling
  terminal, and skips otherwise rather than passing vacuously.
- The compactor is not part of the suspend registry, so a suspend does not stop
  it. It is bounded by its own run and cancellation, cannot touch the terminal,
  and its existing cancellation paths (Ctrl+C, quit, `/exit`, Ctrl+N) still stop
  it. A terminal hangup therefore no longer reaches it either; like the engine,
  it can outlive an abruptly dead REPL until it finishes.

## More Information

- [ADR 018](018-recover-terminal-ownership-around-an-external-suspend.md)
  establishes terminal ownership across a suspend and is partially superseded
  here only in how the engine child is stopped.
- [ADR 019](019-cancel-and-bound-the-read-only-session-listing.md) defines the
  read-only listing lifecycle that this decision extends with the same process
  topology.
- Task 124 tracks bringing the opt-in compactor under the same process boundary.
  See the [amendment](#amendment-2026-10-27) above for its outcome.
- [ADR 020](020-repaint-the-frame-after-content-changes-to-heal-terminal-side-scrolls.md)
  and [ADR 022](022-generate-osc-8-hyperlinks-for-wrapped-assistant-urls.md)
  describe other terminal-boundary behavior that this decision does not change.
- Task 093 records the reproduction and the acceptance evidence; the
  [ExecPlan](../exec-plans/completed/027-isolate-engine-subprocesses.md) carries the
  implementation steps.
- The relevant guardrail is
  [sessions, security, and subprocess lifecycle](../guardrails/session-and-security.md);
  `ARCHITECTURE.md` describes the engine boundary.
