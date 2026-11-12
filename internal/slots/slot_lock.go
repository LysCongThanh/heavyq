// Package slots implements execution slots as flock(2) lock files.
//
// The locks are plain advisory flocks on regular files, so they interoperate
// with `flock /path/to/file cmd` users: a legacy user holding the file blocks
// heavyq and vice versa (legacy users simply get no priority).
//
// Lock files are opened with O_CLOEXEC (Go's default), so commands started by
// heavyq never inherit a slot descriptor. A daemon left behind by a job can
// therefore never keep a slot busy after the job's heavyq process exits.
//
// Lock files often live in a temp directory that an age-based cleaner sweeps
// without looking at flocks (macOS clean-tmps and dirhelper, tmpreaper). A
// held lock therefore keeps its file fresh (lock_freshness.go), and every lock
// checks after flock that it holds the file still at its path.
package slots

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

// Test seams: beforeFlock runs between opening a lock file and flocking it,
// refreshEvery is how often a held lock file is touched.
var (
	beforeFlock  = func(string) {}
	refreshEvery = time.Hour
)

// maxRelock bounds the retries when the file at the path keeps changing
// between open and flock; reaching it means something deletes lock files in
// a loop, and refusing beats locking a file nobody else can see.
const maxRelock = 16

// Lock is a held slot.
type Lock struct {
	Path    string
	mu      sync.Mutex
	f       *os.File
	refresh *time.Timer
}

// TryLock attempts a non-blocking exclusive flock on path, creating the file
// (and its directory) if needed. It returns (nil, nil) when the slot is busy.
func TryLock(path string) (*Lock, error) {
	return lock(path, syscall.LOCK_EX|syscall.LOCK_NB)
}

func lock(path string, how int) (*Lock, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("slot dir: %w", err)
	}
	for range maxRelock {
		f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
		if err != nil {
			// A legacy lock file owned by someone else may be read-only for us.
			f, err = os.Open(path)
			if err != nil {
				return nil, fmt.Errorf("open slot %s: %w", path, err)
			}
		}
		beforeFlock(path)
		if err := flock(f, how); err != nil {
			f.Close()
			if errors.Is(err, syscall.EWOULDBLOCK) {
				return nil, nil
			}
			return nil, fmt.Errorf("flock %s: %w", path, err)
		}
		if stillAt(f, path) {
			l := &Lock{Path: path, f: f}
			l.keepFresh()
			return l, nil
		}
		// The file was unlinked (or replaced) between our open and our flock,
		// or while we waited: we hold an inode nobody else will ever open.
		// Drop it and lock the file that is at the path now.
		f.Close()
	}
	return nil, fmt.Errorf("lock %s: the file keeps being replaced", path)
}

// stillAt reports whether f is the file at path right now.
func stillAt(f *os.File, path string) bool {
	held, err := f.Stat()
	if err != nil {
		return false
	}
	now, err := os.Stat(path)
	return err == nil && os.SameFile(held, now)
}

func flock(f *os.File, how int) error {
	for {
		err := syscall.Flock(int(f.Fd()), how)
		if !errors.Is(err, syscall.EINTR) {
			return err
		}
	}
}

// Release unlocks and closes the slot. Safe on nil and repeatedly.
func (l *Lock) Release() {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.f == nil {
		return
	}
	if l.refresh != nil {
		l.refresh.Stop()
	}
	_ = syscall.Flock(int(l.f.Fd()), syscall.LOCK_UN)
	_ = l.f.Close()
	l.f = nil
}
