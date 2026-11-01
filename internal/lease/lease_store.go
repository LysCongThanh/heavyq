// Package lease lets parallel agents, in different worktrees of one repo,
// claim items without ever working on the same one.
//
// Layout, shared by every worktree and never committed:
//
//	<git-common-dir>/heavyq/leases/<item>.json   one lease per claimed item (0600)
//	<git-common-dir>/heavyq/leases/.lock         directory flock for multi-step operations
//	<git-common-dir>/heavyq/events.jsonl         append-only log: claim, renew, release, reclaim
//
// Every claim, renew, release and reclaim runs under the directory flock, so
// N goroutines or N processes calling ClaimNext never receive the same item.
// Lease files are replaced atomically (temp file + rename), so readers such
// as List need no lock and never see half-written files.
//
// A lease is stale when it has expired (TTL, default 30 min, renewable) or
// when its owner process is gone (pid + start token, immune to pid reuse).
// Claim and ClaimNext reclaim stale leases and log a reclaim event.
// See docs/leases.md for the owner model and caveats.
package lease

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"time"

	"github.com/LysCongThanh/heavyq/internal/procinfo"
	"github.com/LysCongThanh/heavyq/internal/slots"
)

// Store is the lease directory of one repository.
type Store struct {
	// TTL is the lifetime granted by Claim and Renew (default DefaultTTL).
	TTL time.Duration
	// LockTimeout bounds the wait for the directory flock (default 10 s).
	LockTimeout time.Duration

	dir        string // <common>/heavyq/leases
	eventsPath string // <common>/heavyq/events.jsonl
	lockPath   string
	now        func() time.Time
	alive      func(pid int, start string) bool
}

// Open returns the store under commonDir (the output of
// `git rev-parse --git-common-dir`), creating heavyq/leases with mode 0700.
// commonDir itself must already exist: Open never invents a repository.
func Open(commonDir string) (*Store, error) {
	if commonDir == "" {
		return nil, errors.New("lease: git common dir is required")
	}
	abs, err := filepath.Abs(commonDir)
	if err != nil {
		return nil, fmt.Errorf("lease: %w", err)
	}
	if fi, err := os.Stat(abs); err != nil || !fi.IsDir() {
		return nil, fmt.Errorf("lease: git common dir %q is not a directory", commonDir)
	}
	base := filepath.Join(abs, "heavyq")
	dir := filepath.Join(base, "leases")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("lease: create store: %w", err)
	}
	return &Store{
		TTL:         DefaultTTL,
		LockTimeout: 10 * time.Second,
		dir:         dir,
		eventsPath:  filepath.Join(base, "events.jsonl"),
		lockPath:    filepath.Join(dir, ".lock"),
		now:         time.Now,
		alive:       procinfo.Default.Alive,
	}, nil
}

// withLock runs fn holding the directory flock. It polls instead of blocking
// so a stopped lock holder cannot hang its peers forever.
func (s *Store) withLock(fn func() error) error {
	deadline := time.Now().Add(s.LockTimeout)
	for {
		l, err := slots.TryLock(s.lockPath)
		if err != nil {
			return fmt.Errorf("lease lock: %w", err)
		}
		if l != nil {
			defer l.Release()
			return fn()
		}
		if time.Now().After(deadline) {
			return ErrLockTimeout
		}
		time.Sleep(time.Duration(1+rand.IntN(4)) * time.Millisecond)
	}
}

func (s *Store) path(item string) string { return filepath.Join(s.dir, fileName(item)) }

// read loads the lease of item. It returns ErrNotFound when absent and an
// error wrapping errCorrupt when the file is unreadable as a lease for item.
func (s *Store) read(item string) (Lease, error) {
	b, err := os.ReadFile(s.path(item))
	if errors.Is(err, os.ErrNotExist) {
		return Lease{}, ErrNotFound
	}
	if err != nil {
		return Lease{}, fmt.Errorf("read lease %s: %w", item, err)
	}
	var l Lease
	if err := json.Unmarshal(b, &l); err != nil || l.Item != item || l.Pid <= 0 || l.Lane == "" {
		return Lease{}, fmt.Errorf("%w: %s", errCorrupt, item)
	}
	return l, nil
}

// write replaces the lease file atomically with mode 0600.
func (s *Store) write(l Lease) error {
	l.Stale = false
	b, err := json.Marshal(l)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(s.dir, ".tmp-*") // CreateTemp uses 0600
	if err != nil {
		return fmt.Errorf("write lease %s: %w", l.Item, err)
	}
	_, werr := tmp.Write(append(b, '\n'))
	if werr == nil {
		werr = tmp.Sync()
	}
	cerr := tmp.Close()
	if werr == nil {
		werr = cerr
	}
	if werr == nil {
		werr = os.Rename(tmp.Name(), s.path(l.Item))
	}
	if werr != nil {
		os.Remove(tmp.Name())
		return fmt.Errorf("write lease %s: %w", l.Item, werr)
	}
	return nil
}

// staleReason returns "expired", "dead" or "" (live) for l.
func (s *Store) staleReason(l Lease, now time.Time) string {
	switch {
	case !now.Before(l.Expires):
		return "expired"
	case !s.alive(l.Pid, l.Start):
		return "dead"
	}
	return ""
}
