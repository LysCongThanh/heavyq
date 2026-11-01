//go:build !linux && !darwin

package gitutil

import "io/fs"

// fileStamp is unavailable here, so a file too big to hash makes the tree
// unidentifiable instead of being identified by weaker evidence.
func fileStamp(fs.FileInfo) (ino uint64, ctimeNanos int64, ok bool) { return 0, 0, false }
