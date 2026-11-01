//go:build linux

package cli

import "syscall"

const prSetChildSubreaper = 36 // PR_SET_CHILD_SUBREAPER

// becomeSubreaper makes processes orphaned by the command (a daemon its
// shell started, a grandchild whose parent died) re-parent to heavyq instead
// of init, so a cancel still finds them as our descendants.
func becomeSubreaper() bool {
	_, _, errno := syscall.RawSyscall(syscall.SYS_PRCTL, prSetChildSubreaper, 1, 0)
	return errno == 0
}
