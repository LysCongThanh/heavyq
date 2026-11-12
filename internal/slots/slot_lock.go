// Package slots implements execution slots as flock(2) lock files.
//
// The locks are plain advisory flocks on regular files, so they interoperate
// with `flock /path/to/file cmd` users: a legacy user holding the file blocks
// heavyq and vice versa (legacy users simply get no priority).
//
// Lock files are opened with O_CLOEXEC (Go's default), so commands started by
// heavyq never inherit a slot descriptor. A daemon left behind by a job can
// therefore never keep a slot busy after the job's heavyq process exits.
package slots

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// Test seams: beforeFlock runs between opening a lock file and flocking it,
// refreshEvery is how often a held lock file is touched.
var (
	beforeFlock  = func(string) {}
	refreshEvery = time.Hour
)

// Lock is a held slot.
type Lock struct {
	Path string
	f    *os.File
}

// TryLock attempts a non-blocking exclusive flock on path, creating the file
// (and its directory) if needed. It returns (nil, nil) when the slot is busy.
func TryLock(path string) (*Lock, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("slot dir: %w", err)
	}
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		// A legacy lock file owned by someone else may be read-only for us.
		f, err = os.Open(path)
		if err != nil {
			return nil, fmt.Errorf("open slot %s: %w", path, err)
		}
	}
	beforeFlock(path)
	if err := flockNB(f); err != nil {
		f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, nil
		}
		return nil, fmt.Errorf("flock %s: %w", path, err)
	}
	return &Lock{Path: path, f: f}, nil
}

func flockNB(f *os.File) error {
	for {
		err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if !errors.Is(err, syscall.EINTR) {
			return err
		}
	}
}

// Release unlocks and closes the slot. Safe on nil.
func (l *Lock) Release() {
	if l == nil || l.f == nil {
		return
	}
	_ = syscall.Flock(int(l.f.Fd()), syscall.LOCK_UN)
	_ = l.f.Close()
	l.f = nil
}

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
