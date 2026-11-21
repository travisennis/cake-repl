---
status: accepted
date: 2026-11-20
---
# Isolate and bound every external child process

## Context and Problem Statement

The REPL spawns the live cake run, the read-only replay, `cake sessions list`,
`cake trust --status`, the external compactor, and the prompt suggester. A child
that inherits the controlling terminal can change its modes, read its input, or
write over the REPL's frames. A child that ignores termination can hold the REPL
open after the UI is gone.

## Decision

Every external child goes through `internal/childproc`.

Isolation:

- On POSIX, `Isolate` sets `SysProcAttr.Setsid`, so the child and its
  descendants have no controlling terminal. `stdin` is the null device;
  `stdout` and `stderr` are captured pipes.
- A start failure after isolation is fatal. There is no unisolated retry.
- Windows has no equivalent session boundary today; that gap is tracked as
  #10.

Teardown:

- `cmd.Cancel` sends SIGTERM on POSIX and kills on Windows.
- A REPL-owned grace timer sends SIGKILL to the process group after three
  seconds. `cmd.WaitDelay` is set one second later as a PID-only fallback, not
  as the primary bound.
- The process-level `childproc.Registry` tracks each operation by a done
  channel. `main` owns it so an Update or View panic, which returns no model,
  still cancels and reaps every child.
- `ChildTeardownTimeout` bounds the whole unobserved-exit shutdown at five
  seconds. A child that misses the deadline is named in the returned error and
  remains observed by its own goroutine.
- Read-only helpers kill the process group outright; they hold no state to
  flush.

Classification:

- Cancellation is decided from whether `cmd.Cancel` successfully signaled the
  child plus the final process status. It is never decided from `ctx.Err()`
  alone, because a late Ctrl+C after a natural exit must not relabel a failure.

## Consequences

- Good: a child cannot take over the terminal.
- Good: a wedged child cannot hold the process open indefinitely.
- Bad: the same setup is repeated by six callers; #7 tracks one shared runner.
- Bad: Windows isolation is not implemented; #10 tracks it.

## More Information

- [`internal/childproc`](../../internal/childproc)
- Live runner: [`internal/cake/runner.go`](../../internal/cake/runner.go)
- Related: [ADR 050](050-own-the-terminal-around-suspend-and-exit.md)
