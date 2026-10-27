//go:build windows

package cake

import "os/exec"

// configureProcess is a no-op on Windows, which has no POSIX session or
// controlling-terminal equivalent. A child can still reach the console through
// CONIN$ and CONOUT$, so this boundary claims no Windows isolation. See ADR 027.
func configureProcess(_ *exec.Cmd) {}
