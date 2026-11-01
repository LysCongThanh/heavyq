//go:build darwin

package gitutil

import (
	"io/fs"
	"syscall"
)

// fileStamp is the inode and change time of a file. The kernel sets the change
// time on every write and no user-space call can set it back, unlike mtime.
func fileStamp(fi fs.FileInfo) (ino uint64, ctimeNanos int64, ok bool) {
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, 0, false
	}
	return st.Ino, st.Ctimespec.Nano(), true
}
