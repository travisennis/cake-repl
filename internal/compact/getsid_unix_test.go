//go:build !windows && !linux

package compact

import "syscall"

// getSID returns the session id for pid. macOS and the BSDs expose getsid(2)
// directly through package syscall.
func getSID(pid int) (int, error) {
	return syscall.Getsid(pid)
}
