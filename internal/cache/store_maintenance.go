package cache

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// staleTemp is how old an unfinished recording must be before eviction
// removes it (its writer is gone or the run is absurdly long).
const staleTemp = 24 * time.Hour

// List returns all complete entries, newest first (expired ones included;
// see Expired).
func (s *Store) List() ([]Meta, error) {
	ents, err := os.ReadDir(s.Dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Meta
	for _, e := range ents {
		key, ok := strings.CutSuffix(e.Name(), ".json")
		if !ok || !keyRe.MatchString(key) {
			continue
		}
		b, err := os.ReadFile(filepath.Join(s.Dir, e.Name()))
		if err != nil {
			continue
		}
		var m Meta
		if json.Unmarshal(b, &m) == nil && m.Key == key {
			out = append(out, m)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Created.After(out[j].Created) })
	return out, nil
}

// Expired reports whether m is past the TTL.
func (s *Store) Expired(m Meta, now time.Time) bool { return now.Sub(m.Created) > s.TTL }

// Evict removes expired entries, orphans and stale temp files, then the
// oldest entries until the total size fits MaxBytes.
func (s *Store) Evict(now time.Time) error {
	unlock, err := s.lock()
	if err != nil {
		return err
	}
	defer unlock()
	return s.evictLocked(now)
}

func (s *Store) evictLocked(now time.Time) error {
	ents, err := os.ReadDir(s.Dir)
	if err != nil {
		return err
	}
	metas, err := s.List()
	if err != nil {
		return err
	}
	live := map[string]bool{}
	for _, m := range metas {
		live[m.Key] = true
	}
	for _, e := range ents {
		n := e.Name()
		if key, ok := strings.CutSuffix(n, ".out"); ok && keyRe.MatchString(key) && !live[key] {
			s.removeIfOld(n, now, time.Minute) // a commit renames .out just before writing .json
		} else if strings.HasPrefix(n, ".rec-") || strings.HasSuffix(n, ".json.tmp") {
			s.removeIfOld(n, now, staleTemp)
		}
	}
	var total int64
	for _, m := range metas {
		total += m.Size
	}
	// metas is newest first: drop expired ones, then the oldest until it fits.
	for i := len(metas) - 1; i >= 0; i-- {
		m := metas[i]
		if s.Expired(m, now) || total > s.MaxBytes {
			s.remove(m.Key)
			total -= m.Size
		}
	}
	return nil
}

func (s *Store) removeIfOld(name string, now time.Time, age time.Duration) {
	if fi, err := os.Stat(filepath.Join(s.Dir, name)); err == nil && now.Sub(fi.ModTime()) > age {
		_ = os.Remove(filepath.Join(s.Dir, name))
	}
}

func (s *Store) remove(key string) {
	_ = os.Remove(s.path(key, ".json")) // first: the entry stops being visible
	_ = os.Remove(s.path(key, ".out"))
}

// Clear removes every entry and returns how many there were.
func (s *Store) Clear() (int, error) {
	unlock, err := s.lock()
	if err != nil {
		return 0, err
	}
	defer unlock()
	metas, err := s.List()
	if err != nil {
		return 0, err
	}
	for _, m := range metas {
		s.remove(m.Key)
	}
	return len(metas), s.evictLocked(time.Now())
}
