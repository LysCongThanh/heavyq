//go:build darwin

package slots

import (
	"io"
	"os"
	"syscall"
)

// heldByAnyone asks fcntl(F_GETLK) whether a write lock on the whole file
// would conflict. On macOS flock(2) and fcntl locks share one lock list, and
// F_GETLK reports a flock held through any other open file (with pid -1), so
// it sees heavyq, flock(1) and Perl holders without taking anything.
func heldByAnyone(path string) (busy, known bool) {
	f, err := os.Open(path)
	if err != nil {
		return false, false
	}
	defer f.Close()
	lk := syscall.Flock_t{Type: syscall.F_WRLCK, Whence: io.SeekStart}
	if err := syscall.FcntlFlock(f.Fd(), syscall.F_GETLK, &lk); err != nil {
		return false, false
	}
	return lk.Type != syscall.F_UNLCK, true
}
