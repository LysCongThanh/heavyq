package slots

import "os"

// Busy reports whether path is currently locked by someone.
//
// status, why and the dashboard call it without the queue mutex, and
// pools.Check calls it in front of every real Acquire, so it must not take
// the lock: a run (or a legacy `flock -n` user) that tried the slot while a
// probe held it would see a free slot as busy. heldByAnyone asks the kernel
// instead (lock_holders_*.go). Only when the kernel's answer is unavailable
// or ambiguous does Busy fall back to taking and releasing the lock.
func Busy(path string) bool {
	if _, err := os.Stat(path); err != nil {
		return false
	}
	if busy, known := heldByAnyone(path); known {
		return busy
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
