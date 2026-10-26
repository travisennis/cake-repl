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

Chosen option: every POSIX child the REPL spawns for cake runs in a new session
with no controlling terminal, and the lifecycle consequences are handled
explicitly.

- **Mechanism.** `internal/cake` applies one process-attribute helper to the live
  run, replay, and session listing: `SysProcAttr{Setsid: true}` on `!windows`.
  Windows leaves the attribute unset. One helper, not three ad-hoc assignments,
  keeps the policy from drifting between spawn sites.
- **Cancellation.** SIGTERM still goes to the child first, so cake can finish
  writing session records and stop its tools. If the child has not been reaped
  when the existing three-second `WaitDelay` expires, the REPL sends SIGKILL to
  the child's process group. `exec`'s own `WaitDelay` kill is PID-only, so the
  group kill needs a REPL-owned timer. A child that exits gracefully is not
  followed by a group signal: a descendant that deliberately daemonized (a dev
  server started by a tool) should outlive the turn, exactly as it does today.
- **Suspension.** The suspend cycle keeps ADR 018's order and intent but adds
  explicit engine coordination: release the terminal, stop each live run's
  process group with SIGSTOP, stop the REPL's own group (unchanged), and after
  SIGCONT continue the engine groups before re-acquiring the terminal and
  repainting. The REPL tracks live and replay runs for this purpose; the
  read-only session listing is short-lived and is not tracked. A REPL killed
  while stopped can still leave an engine stopped; that remains the documented
  ADR 018 limit.
- **Start failure is fail-closed.** If the child cannot be started with the
  attribute, starting the run fails and the error is shown. There is no silent
  unisolated retry.
- **Signals.** After `Setsid` the terminal no longer delivers Ctrl+C or SIGTSTP
  to cake; only the REPL receives them. Ctrl+C reaches cake as the REPL's
  SIGTERM, which cake handles like its own Ctrl+C, so cancellation behavior is
  unchanged. Ctrl+Z reaches cake as the explicit SIGSTOP from the suspend cycle.
- **Tools that need a terminal now fail.** `sudo`, an SSH passphrase prompt,
  `gh auth login`, or an editor that opens `/dev/tty` receives `ENXIO` instead of
  prompting over the REPL. That is accepted: a prompt raised by a tool must be
  surfaced through cake's stream, and no opt-out flag is added in this change.
- **Partial supersession of ADR 018.** ADR 018 says the process group is stopped
  "so the cake child pauses with the REPL". The intent still holds; the mechanism
  no longer does. ADR 018 keeps its accepted status, and this ADR records the
  replacement of that one mechanism.

The explicit non-goals are a named terminal device (`/dev/ttysNNN`), signals from
another same-user process, a descendant that calls `setsid` itself, Windows
console handles, and any claim of a general sandbox. The opt-in `cake-compact`
child in `internal/compact` is outside this task's scope and keeps its current
topology; it is a known gap rather than an implied guarantee.

### Consequences

- Good: engine children and their descendants cannot change the REPL's terminal
  modes, read its input, or write over its frames through the controlling
  terminal. The reproduced `stty sane < /dev/tty` wrapper fails instead of
  corrupting the pane.
- Good: cancellation now reaps a hung child's whole process group, while a
  graceful exit still allows intentionally backgrounded tools to survive.
- Good: session identity is observable in tests without a terminal: a detached
  child's session id equals its own pid, while the REPL's does not.
- Bad: tools that require a terminal now fail rather than prompt.
- Bad: the compactor remains a second, unaddressed terminal path until it is
  brought under the same helper.
- Bad: Windows gets no terminal-isolation guarantee; the existing topology is
  documented as the limit.

## More Information

- [ADR 018](018-recover-terminal-ownership-around-an-external-suspend.md)
  establishes terminal ownership across a suspend and is partially superseded
  here only in how the engine child is stopped.
- [ADR 019](019-cancel-and-bound-the-read-only-session-listing.md) defines the
  read-only listing lifecycle that this decision extends with the same process
  topology.
- [ADR 020](020-repaint-the-frame-after-content-changes-to-heal-terminal-side-scrolls.md)
  and [ADR 022](022-generate-osc-8-hyperlinks-for-wrapped-assistant-urls.md)
  describe other terminal-boundary behavior that this decision does not change.
- Task 093 records the reproduction and the acceptance evidence; the
  [ExecPlan](../exec-plans/active/027-isolate-engine-subprocesses.md) carries the
  implementation steps.
- The relevant guardrail is
  [sessions, security, and subprocess lifecycle](../guardrails/session-and-security.md);
  `ARCHITECTURE.md` describes the engine boundary.
