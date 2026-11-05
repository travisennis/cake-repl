---
status: accepted
date: 2026-11-03
decision-makers: Travis Ennis
---
# Bound unobserved-exit child teardown

## Context and Problem Statement

Bubble Tea can return the final model without delivering a final `Update`, for
example on SIGINT or SIGTERM. `main` then calls `Model.CancelRunning` to stop
children the program loop can no longer observe. Before this decision,
`CancelRunning` canceled the live run, replay, compaction, suggestion, and
session listing, but only waited for the suggester. The run and compaction
children could still be exiting, and a replay that was still starting had no
cancel handle at all. A run's SIGKILL grace timer also lives in the REPL
process, so a REPL exit could drop the timer while the child was still running.

The shutdown path must stop and reap every child without relying on a dead
Update loop, and it must not block forever on a wedged child.

## Decision Drivers

- No model-owned child may outlive the REPL on a supported exit path.
- Shutdown must be bounded even when a child ignores termination.
- A child's result must not be required to prove it was reaped; the Update loop
  that normally consumes results may already be gone.
- Normal cancellation should keep delivering events while the Update loop is
  still draining them.
- Preserve the existing SIGTERM, process-group SIGKILL, and `WaitDelay` policy
  from ADR 027.
- Tests must use fake children and must not require a real cake binary.

## Considered Options

- **Wait on each child's result channel.** Rejected: the result may be consumed
  by an in-flight Bubble Tea command, and a dead Update loop cannot process it.
- **Give each child its own shutdown timeout.** Rejected: several slow children
  would multiply the exit delay.
- **Return at the deadline without reporting the miss.** Rejected: a child could
  still be alive with no visible indication.
- **Reaping signals plus one shared deadline.** Chosen: each child exposes a
  channel that closes when it has been reaped, and `CancelRunning` waits on all
  of them under one fixed deadline.

## Decision Outcome

Chosen option: "Reaping signals plus one shared deadline", because it proves
child teardown without competing with result delivery and bounds the whole exit.

- `cake.Run` gains `Done`, closed after `cmd.Wait` reaps the child. It is
  independent of `Result` and may close before the result is consumed.
- After cancellation, the runner keeps draining stdout. Event sends try a
  non-blocking send first, then block on either the event channel or the
  canceled context. Only a blocked post-cancel send is dropped, so a full
  `Events` channel cannot prevent `Wait` from running while normal cancellation
  still delivers events to a live Update loop.
- Every model-owned operation has a reaping signal: the live run uses
  `Run.Done`; startup replay owns a context and a done channel because its
  `Run` may not have reached the model yet; compaction, suggestion, and each
  session listing close their own done channel after the child returns.
- `Model.CancelRunning` cancels all operations, then waits on every reaping
  signal under one five-second deadline. It returns an error naming operations
  that missed the deadline; `main` reports that error and exits non-zero.
- Normal quit paths keep their existing result-draining behavior. `main` runs
  `CancelRunning` after every `p.Run` return, so a quit that left a compactor or
  listing draining also gets the bounded reaping wait; a fully settled quit has
  no operations to wait for.
- The startup replay's done channel and the startup listing's done channel
  exist before Bubble Tea
  schedules their commands. If a command is never scheduled (for example,
  terminal initialization fails first), shutdown can wait the full deadline and
  report a timeout even though no child exists. The wait is bounded, and
  skipping unstarted operations could miss a child that starts after shutdown
  begins; accepted.
- An `Update` or `View` panic returns no model, so `main` cannot call
  `CancelRunning` on that path. A process-level child registry is required to
  cover it; task 149 tracks that follow-up.

### Consequences

- Good: a full event buffer no longer leaves the runner blocked before reaping.
- Good: one deadline bounds the total exit delay across all children.
- Good: a missed deadline is visible on stderr and in the exit status.
- Bad: a command that was never scheduled can be reported as a timeout after
  the full deadline; accepted as bounded and limited to startup failure or a
  very early signal.
- Bad: an `Update` or `View` panic still has no child cleanup; task 149 tracks
  the registry needed to close that path.
- Neutral: no CLI flag, config shape, stream-json schema, or normal-quit
  behavior changes.

## Amendment (2026-11-03)

Task 149 closes the nil-model gap recorded above. `cmd/cake-repl` now creates a
process-level `childproc.Registry` before starting the Bubble Tea program and
passes it through `app.Config.Children`. The app registers every child operation
as it starts: the live run, startup replay, compaction, suggestion, and each
session listing. After `p.Run` returns, `main` performs the same bounded
shutdown whether or not Bubble Tea returned a model; `Model.CancelRunning`
delegates to the registry when one is wired. The five-second deadline, the wait
on reaping signals, and the non-zero exit on a missed deadline are unchanged.
Registrations after shutdown begins are canceled rather than admitted. A panic
in `Update` or `View` can no longer leave a child running.

## More Information

- [ADR 027](027-isolate-engine-subprocesses-from-the-repl-controlling-terminal.md)
  owns terminal isolation, SIGTERM, the process-group SIGKILL grace, and
  `WaitDelay`.
- [ADR 019](019-cancel-and-bound-the-read-only-session-listing.md) owns the
  read-only listing's cancellation and bounded teardown.
- Task 134 implements this decision. Task 149 closes the nil-model gap with the
  process-level registry described in the 2026-11-03 amendment.
