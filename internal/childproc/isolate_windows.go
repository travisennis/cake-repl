//go:build windows

package childproc

import "os/exec"

// Isolate is a no-op on Windows, which has no POSIX session or
// controlling-terminal equivalent. CONIN$ and CONOUT$ remain reachable, so this
// boundary claims no Windows isolation. See ADR 027.
func Isolate(_ *exec.Cmd) {}
