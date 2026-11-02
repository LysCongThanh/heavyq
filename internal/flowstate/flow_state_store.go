// Package flowstate is the I/O around internal/flow: the materialised state at
// <git common dir>/heavyq/flow/state.json (atomic, debounced, written under
// flock) and the append-only log of hook observations beside it.
//
// Nothing here computes a flow; Refresh calls the function it is given. The
// state is read by hot paths (the status line, top, the brief): Read is one
// small file read and a decode, measured in tests to stay far under 5 ms.
package flowstate

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/LysCongThanh/heavyq/internal/flow"
)

// DefaultDebounce is the least time between two writes of state.json by
// routine refreshes. A needs-human event forces one anyway (see Refresh).
const DefaultDebounce = 60 * time.Second

// lockWait is how long a forced refresh waits for a refresh already running.
const lockWait = 3 * time.Second

// Dir is where the flow files of the repository with this git common dir live.
func Dir(common string) string { return filepath.Join(common, "heavyq", "flow") }

// StatePath is state.json.
func StatePath(common string) string { return filepath.Join(Dir(common), "state.json") }

// Read returns the materialised state. A repository that has none yet is
// (nil, nil): callers show "no flow data", not an error.
func Read(common string) (*flow.Snapshot, error) {
	b, err := os.ReadFile(StatePath(common))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("flow state: %w", err)
	}
	var s flow.Snapshot
	if err := json.Unmarshal(b, &s); err != nil {
		return nil, fmt.Errorf("flow state %s: %w", StatePath(common), err)
	}
	return &s, nil
}

// Store writes the state of one repository.
type Store struct {
	Common   string
	Debounce time.Duration    // 0: DefaultDebounce
	Now      func() time.Time // nil: time.Now
}

func (s Store) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s Store) debounce() time.Duration {
	if s.Debounce > 0 {
		return s.Debounce
	}
	return DefaultDebounce
}

// Refresh brings state.json up to date with what compute derives, and reports
// whether it wrote.
//
// Under an exclusive flock of the flow directory, so that however many hooks and
// runs fire at once exactly one computes and writes:
//
//   - a routine refresh (force false) does nothing while another refresh holds the
//     lock, and nothing while the file is younger than the debounce (at most one
//     write per 60 s);
//   - a forced refresh waits up to three seconds for the lock and writes at
//     once. Hooks force when a person is needed or an answer arrived: those
//     are rare and must show within one event, not within a minute.
//
// The write is a temp file renamed over state.json, so a reader sees the old
// or the new state, never half of one. A state stamped in the future (a clock
// step) counts as stale.
func (s Store) Refresh(force bool, compute func() (flow.Snapshot, error)) (bool, error) {
	if err := os.MkdirAll(Dir(s.Common), 0o700); err != nil {
		return false, fmt.Errorf("flow state: %w", err)
	}
	unlock, err := s.lock(force)
	if err != nil {
		return false, err
	}
	if unlock == nil {
		return false, nil // another refresh is running: it is the one that writes
	}
	defer unlock()
	if !force {
		if cur, err := Read(s.Common); err == nil && cur != nil {
			if age := s.now().Sub(cur.Updated); age >= 0 && age < s.debounce() {
				return false, nil
			}
		}
	}
	snap, err := compute()
	if err != nil {
		return false, err
	}
	return true, writeAtomic(StatePath(s.Common), snap)
}

// lock takes the directory lock. A routine refresh gets (nil, nil) when it is
// busy; a forced one retries until lockWait.
func (s Store) lock(force bool) (func(), error) {
	f, err := os.OpenFile(filepath.Join(Dir(s.Common), "state.lock"), os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, fmt.Errorf("flow state lock: %w", err)
	}
	deadline := time.Now().Add(lockWait)
	for {
		err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN); f.Close() }, nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) {
			f.Close()
			return nil, fmt.Errorf("flow state lock: %w", err)
		}
		if !force {
			f.Close()
			return nil, nil
		}
		if time.Now().After(deadline) {
			f.Close()
			return nil, fmt.Errorf("flow state lock: busy for %s", lockWait)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// writeAtomic marshals v and renames it over path.
func writeAtomic(path string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("flow state: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".state-*")
	if err != nil {
		return fmt.Errorf("flow state: %w", err)
	}
	_, werr := tmp.Write(append(b, '\n'))
	if err := errors.Join(werr, tmp.Close()); err != nil {
		os.Remove(tmp.Name())
		return fmt.Errorf("flow state: %w", err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		os.Remove(tmp.Name())
		return fmt.Errorf("flow state: %w", err)
	}
	return nil
}
