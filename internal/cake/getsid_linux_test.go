//go:build linux

package cake

import "syscall"

// getSID returns the session id for pid. Linux does not expose getsid(2) in
// package syscall, so call SYS_GETSID directly; the rest of the test file stays
// portable.
func getSID(pid int) (int, error) {
	r, _, errno := syscall.Syscall(syscall.SYS_GETSID, uintptr(pid), 0, 0)
	if errno != 0 {
		return 0, errno
	}
	return int(r), nil
}
