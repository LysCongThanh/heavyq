package ledgerindex

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/LysCongThanh/heavyq/internal/history"
)

// Append writes rec to the ledger at path (history.Append) and indexes it.
// This is the single write path of the index; callers use it wherever they
// used history.Append.
//
// Only the ledger write can fail the call. The index is derived data: when it
// cannot be updated (no lock within lockWait, a full disk, a corrupt file) the
// record is still in the ledger, meta.json no longer matches the ledger's size,
// readers fall back to ReadTail, and the next Append or `doctor --reindex`
// repairs it.
func Append(path string, rec history.Record) error {
	if path == "" {
		return history.Append(path, rec)
	}
	dir := Dir(path)
	unlock, err := lock(dir)
	if err != nil {
		return history.Append(path, rec)
	}
	defer unlock()
	s, serr := syncIndex(path, dir)
	if err := history.Append(path, rec); err != nil {
		return err
	}
	if serr == nil {
		serr = s.catchUp(path)
	}
	if serr != nil { // a file the index cannot read or apply: start over from the ledger
		s, serr = rebuild(path)
	}
	if serr == nil {
		serr = s.flush(dir)
	}
	if serr != nil {
		_ = os.Remove(filepath.Join(dir, "meta.json")) // never leave a half-updated index looking fresh
	}
	return nil
}

// Reindex rebuilds the whole index from the ledger at path under the writers'
// lock and returns its stats and how long it took.
func Reindex(path string) (Stats, time.Duration, error) {
	start := time.Now()
	dir := Dir(path)
	unlock, err := lock(dir)
	if err != nil {
		return Stats{}, 0, err
	}
	defer unlock()
	s, err := rebuild(path)
	if err != nil {
		return Stats{}, 0, err
	}
	if err := s.flush(dir); err != nil {
		return Stats{}, 0, fmt.Errorf("write index: %w", err)
	}
	st, err := StatsOf(path)
	return st, time.Since(start), err
}

// syncIndex opens the index in dir and brings it up to the end of the ledger.
// When it cannot be trusted (missing, corrupt, another schema, a ledger file
// that shrank or vanished) it is rebuilt from the ledger instead.
func syncIndex(path, dir string) (*state, error) {
	s, err := openState(dir)
	if err == nil {
		if err = s.catchUp(path); err == nil {
			return s, nil
		}
	}
	return rebuild(path)
}

// rebuild indexes the whole ledger into a fresh state (not yet written).
func rebuild(path string) (*state, error) {
	s := emptyState()
	var applyErr error
	last, size, err := history.Each(path, func(r history.Record) {
		if applyErr == nil {
			applyErr = s.apply(r)
		}
	})
	if err = firstErr(applyErr, err); err != nil {
		return nil, fmt.Errorf("read ledger: %w", err)
	}
	s.meta.Ledger, s.meta.Size = filepath.Base(path), 0
	if last != "" {
		s.meta.Ledger, s.meta.Size = filepath.Base(last), size
	}
	return s, nil
}

// catchUp applies the records the ledger has gained since the state was taken
// (from meta's byte offset), across a month change when the ledger rotated.
func (s *state) catchUp(path string) error {
	var applyErr error
	apply := func(r history.Record) {
		if applyErr == nil {
			applyErr = s.apply(r)
		}
	}
	cur := filepath.Base(path)
	if s.meta.Ledger != cur { // rotated: finish the old file, start the new
		old := filepath.Join(filepath.Dir(path), s.meta.Ledger)
		if _, err := history.EachFrom(old, s.meta.Size, apply); err != nil {
			return err
		}
		s.meta.Ledger, s.meta.Size = cur, 0
	}
	size, err := history.EachFrom(path, s.meta.Size, apply)
	if err = firstErr(applyErr, err); err != nil {
		return err
	}
	s.meta.Size = size
	return nil
}

func firstErr(errs ...error) error {
	for _, e := range errs {
		if e != nil {
			return e
		}
	}
	return nil
}
