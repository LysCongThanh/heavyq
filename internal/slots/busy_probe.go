package slots

import "os"

// Busy reports whether path is currently locked by someone. It briefly takes
// and releases the lock when free, so use it for display only.
func Busy(path string) bool {
	if _, err := os.Stat(path); err != nil {
		return false
	}
	l, err := TryLock(path)
	if err != nil {
		return false
	}
	if l == nil {
		return true
	}
	l.Release()
	return false
}
