---
status: accepted
date: 2026-11-20
---
# Own the terminal around suspend and exit

## Context and Problem Statement

Bubble Tea owns the terminal while the program runs, but not every exit goes
through the model. SIGINT and SIGTERM return without running `Update`; a panic in
`Update` or `View` returns no model. An external SIGTSTP also stops the process
without giving the terminal back, so a shell `fg` resumes a REPL that still
believes it owns raw mode.

## Decision

- A catchable external SIGTSTP runs one fixed cycle: release the terminal, park
  the live region, pause tracked engine process groups, stop the process group,
  resume engine groups after SIGCONT, return to the buffer that owns the live
  region, re-enable the modes Bubble Tea does not restore, and re-own the
  terminal.
- The terminal is released before anything stops. Each detached engine run is
  paused before the REPL's own group stops, or engine work keeps running
  unwatched.
- The stop signal is SIGSTOP to the whole group, not a re-raised SIGTSTP. Once
  the program has taken SIGTSTP with `signal.Notify`, the Go runtime keeps it
  blocked and a Reset-and-raise is swallowed. SIGSTOP cannot be caught, ignored,
  or blocked, and `fg` resumes it with SIGCONT.
- Inline mode parks the suspension on the alternate screen. Otherwise the shell
  writes its job-control notice into the live region's rows, and the next frame,
  which moves up by the previous frame's height, lands on the wrong lines.
- Bubble Tea's `RestoreTerminal` restores bracketed paste and focus reporting
  but not mouse reporting. The cycle re-enables the mouse so the wheel keeps
  scrolling the timeline.
- The model writes the idle terminal title after `Program.Run` returns, because
  the model cannot observe SIGINT, SIGTERM, or a panic. Inline mode also erases
  the composer rows after the program returns, so the shell prompt is not
  preceded by a stale input box.
- Terminal control sequences are written only when stdout is a character
  device. Redirected output is left untouched.

## Consequences

- Good: `Ctrl+Z` and `fg` leave a usable terminal in both alternate-screen and
  inline modes.
- Good: a signal or panic exit still resets the title and removes the composer.
- Bad: the suspend cycle is platform-specific; Windows has no equivalent and
  installs a no-op.
- Bad: the ordering is a contract that cannot be inferred from the function
  names; it is recorded here.

## More Information

- [`cmd/cake-repl/suspend.go`](../../cmd/cake-repl/suspend.go)
- [`cmd/cake-repl/main.go`](../../cmd/cake-repl/main.go)
- [`internal/cake/registry.go`](../../internal/cake/registry.go)
- Related: [ADR 041](041-isolate-and-bound-every-external-child-process.md)
