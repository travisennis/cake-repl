// Package childproc applies the process boundary shared by every external
// child the REPL spawns: the live cake run, replay, the session listing, and
// the opt-in compactor.
//
// The boundary is POSIX-only. A child placed in its own session has no
// controlling terminal, so opening /dev/tty fails with ENXIO instead of letting
// the child change the terminal's modes, read its input, or write over the
// REPL's frames. Go already gives such a child the null device as stdin and
// captured pipes for stdout and stderr. Windows has no equivalent, so it is a
// no-op there. See ADR 027.
package childproc
