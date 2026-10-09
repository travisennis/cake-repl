---
status: accepted
date: 2026-10-09
decision-makers: Travis Ennis
---
# Recover terminal ownership around an external suspend

## Context and Problem Statement

cake-repl runs as a Bubble Tea program that puts the terminal into raw mode,
hides the cursor, enables mouse reporting, and — by default — uses the alternate
screen. On its own terms it is the only thing managing those modes: when the
program returns, Bubble Tea restores what it changed.

An **external, catchable SIGTSTP** breaks that assumption. `kill -TSTP <pid>`
from another terminal, a terminal key bound to the signal, or any other hand
that is not this program's own suspended key stops the process wherever it had
reached, with no chance to give the terminal back. The shell then reclaims the
terminal for its own prompt and, on `fg`, hands it to a process that still
believes it owns raw mode. The user sees the shell's job-control notice inside
the managed region, a terminal in cooked mode where keys echo and an Up
arrow arrives as the literal characters `^[[A`, mouse reports that are never
consumed, and a frame repainted from a cursor position the renderer no longer
has.

Bubble Tea v1.3.10 covers only the *internal* path: `tea.Suspend` is driven by
the program's own Ctrl+Z key message and runs the release/restore sequence
itself. The library installs no SIGTSTP handler, so a signal it did not ask for
has no recovery at all. The same is true of `RestoreTerminal`, which restores
raw input, the cursor, bracketed paste, focus reporting, the alternate screen,
and the renderer, but **re-enables no mouse reporting** — the dependency does not
restore everything, so the modes have to be audited rather than assumed.

Reproduction used for this decision (macOS, tmux 3.7c, zsh, the checked-in fake
cake, `-no-color`, both render modes): the pane's own line discipline raises
SIGTSTP for the foreground job (ISIG armed, Ctrl+Z), then `fg` foregrounds it.
Before this decision the resumed REPL ran with `icanon isig echo` enabled and
its mouse-report flag still set, and an Up keypress was echoed into the frame as
`^[[A` instead of recalling history; the shell's `zsh: suspended` and
`[1] + continued` lines stayed inside the managed region.

## Decision Drivers

- Preserve the conversation. A suspension is not a session boundary: the
  timeline, the composer contents, prompt history, and the session pin must
  survive it.
- Never leave the shell with a terminal this process believes it owns, and never
  leave this process believing it holds a terminal a shell has taken back.
- Do not lose work in progress: an engine or replay running at the moment of
  suspension must not be killed, and must not be left running unwatched.
- Depend only on supported library surface, and treat what the library does not
  restore as this project's responsibility.
- Keep the failure recoverable and recorded rather than implied: every case that
  cannot be handled must be written down.

## Considered Options

- **Leave it unsupported.** Document that an external SIGTSTP requires quitting
  and restarting. Rejected: the terminal is left unusable mid-session, and the
  corruption is silent.
- **Catch SIGTSTP, release the terminal, then re-raise SIGTSTP with the default
  disposition.** This is the textbook job-control sequence, and it is what
  Bubble Tea's own `suspend` does internally. Rejected: once a Go program has
  taken SIGTSTP with `signal.Notify`, the runtime keeps the signal blocked, and
  a `signal.Reset` plus re-raise is *swallowed* instead of stopping the process
  (reproduced: the process kept running and `kill` returned success). The
  sequence cannot be trusted to stop anything.
- **Catch SIGTSTP, release the terminal, then stop with SIGSTOP.** Chosen:
  SIGSTOP cannot be caught, ignored, or blocked, so the stop is reliable, and
  `fg` resumes the job with SIGCONT exactly as for a Ctrl+Z stop.

## Decision Outcome

Chosen option: catch SIGTSTP in `cmd/cake-repl`, release the terminal before
stopping, stop the process group with SIGSTOP (which `fg` later continues with
SIGCONT), restore every mode, and repaint without touching the conversation.

- **Release before stopping.** `Program.ReleaseTerminal` restores the original
  termios, shows the cursor, disables mouse reporting and bracketed paste, leaves
  the alternate screen, and stops the renderer. A release failure abandons the
  cycle and leaves the REPL running: stopping with raw mode still enabled is the
  defect, so it is never traded for a "successful" suspend.
- **Stop the process group, not the process.** The stop signal goes to the
  whole process group, so the cake child pauses with the REPL. The engine is
  never killed and never left running while the REPL is stopped; it resumes with
  the same run, delivering the events it produced into the same timeline.
- **Restore every mode, not just the library's.** After SIGCONT, the mouse
  reporting sequences are written while the renderer is still stopped (so no
  frame can interleave and split them), and then `RestoreTerminal` re-owns the
  terminal, which repaints and re-reads the window size — that size check is
  also what repaints an idle REPL, whose empty render buffer would otherwise
  leave a blank frame.
- **Inline mode parks the suspension on the alternate screen.** The alternate
  screen is the default mode, so releasing the terminal already puts the shell
  back on its own screen with the live region and cursor intact. Inline mode has
  none: the shell would write its notice into the live region's rows, and the
  resumed frame would land as many rows high as the shell wrote, or be drawn
  over stale rows. Parking the suspension on the alternate screen gives the
  shell a clean full window and restores the inline region exactly where the
  renderer left it, so the resume repaint rewrites it in place.
- **Only catches what can be caught.** SIGSTOP cannot be intercepted, so a
  suspension delivered that way cannot release the terminal before it happens;
  that case is best-effort and is documented, not implied. Continuing the job in
  the background (`bg`) resumes this process with SIGCONT while a shell owns the
  terminal; the REPL re-owns it anyway, exactly as Bubble Tea's own suspend does.

### Consequences

- Good, because an external SIGTSTP now behaves like a normal job-control
  suspension: the shell gets a usable prompt, and `fg` returns to the same
  conversation with raw input, cursor, mouse, and screen modes intact.
- Good, because a suspended engine is paused rather than killed or orphaned, and
  its output arrives on resume.
- Good, because the fix stays inside `cmd/cake-repl` and uses only the supported
  `ReleaseTerminal`/`RestoreTerminal` surface plus terminal mode sequences, so
  the cake contract, the stream schema, and the app/ui layering are untouched.
- Bad, because inline mode's suspension takes the shell to a full screen whose
  output is discarded when the REPL returns; the user's own shell history is
  restored, but anything run while suspended is not.
- Bad, because the resumed job is reported by the shell as
  `suspended (signal)`, since the stop uses SIGSTOP rather than SIGTSTP.
- Bad, because SIGSTOP-initiated suspension and a killed-while-stopped REPL
  remain unrecoverable, and are documented as such.

## More Information

- Task 092 holds the reproduction, the acceptance criteria, and the evidence.
- Task 093 covers isolating cake subprocesses from the REPL's controlling
  terminal, which is a different boundary from this recovery path.
- Bubble Tea v1.3.10: `signals_unix.go` listens for SIGWINCH only;
  `tty_unix.go` stops the process with `kill(0, SIGTSTP)` for the internal
  suspend; `tea.go` `RestoreTerminal` re-enables bracketed paste and focus
  reporting but not mouse reporting, and ends with a resize check.
