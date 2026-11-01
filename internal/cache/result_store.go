package cache

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"syscall"
	"time"
)

// Meta describes one stored result. An entry is <key>.out (chunks) plus
// <key>.json (this); the .json is written last, so its presence means the
// entry is complete.
type Meta struct {
	Key     string    `json:"key"`
	Created time.Time `json:"created"`
	Exit    int       `json:"exit"`
	Machine string    `json:"machine"`
	Lane    string    `json:"lane"`
	Cmd     string    `json:"cmd"`
	RunSecs float64   `json:"run_s"`
	Size    int64     `json:"size"`
}

// Store is a directory of results with a TTL and a total size cap.
type Store struct {
	Dir           string
	TTL           time.Duration
	MaxBytes      int64
	MaxEntryBytes int64
}

var keyRe = regexp.MustCompile(`^[0-9a-f]{64}$`)

// DefaultDir is $XDG_CACHE_HOME/heavyq/results, else ~/.cache/heavyq/results.
func DefaultDir() string {
	base := os.Getenv("XDG_CACHE_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		base = filepath.Join(home, ".cache")
	}
	return filepath.Join(base, "heavyq", "results")
}

func (s *Store) path(key, ext string) string { return filepath.Join(s.Dir, key+ext) }

// Get returns the entry for key, or nil when missing, unreadable or expired.
func (s *Store) Get(key string, now time.Time) *Meta {
	if !keyRe.MatchString(key) {
		return nil
	}
	b, err := os.ReadFile(s.path(key, ".json"))
	if err != nil {
		return nil
	}
	var m Meta
	if json.Unmarshal(b, &m) != nil || m.Key != key || now.Sub(m.Created) > s.TTL {
		return nil
	}
	return &m
}

// Replay writes the stored output. opened is false when the output file is
// gone (evicted meanwhile): nothing was written, treat it as a miss.
func (s *Store) Replay(m *Meta, stdout, stderr io.Writer) (opened bool, err error) {
	f, err := os.Open(s.path(m.Key, ".out"))
	if err != nil {
		return false, err
	}
	defer f.Close()
	return true, replay(f, stdout, stderr)
}

// NewRecorder starts recording a run whose output goes to stdout/stderr.
func (s *Store) NewRecorder(stdout, stderr io.Writer) (*Recorder, error) {
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return nil, fmt.Errorf("cache dir: %w", err)
	}
	f, err := os.CreateTemp(s.Dir, ".rec-*")
	if err != nil {
		return nil, fmt.Errorf("cache dir: %w", err)
	}
	return &Recorder{f: f, max: s.MaxEntryBytes, stdout: stdout, stderr: stderr}, nil
}

// Commit stores a finished recording under m.Key, then evicts.
func (s *Store) Commit(r *Recorder, m Meta) error {
	if err := r.finish(); err != nil {
		_ = os.Remove(r.f.Name())
		return err
	}
	if !keyRe.MatchString(m.Key) {
		_ = os.Remove(r.f.Name())
		return errors.New("invalid cache key")
	}
	m.Size = r.size
	unlock, err := s.lock()
	if err != nil {
		_ = os.Remove(r.f.Name())
		return err
	}
	defer unlock()
	if err := os.Rename(r.f.Name(), s.path(m.Key, ".out")); err != nil {
		_ = os.Remove(r.f.Name())
		return err
	}
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	tmp := s.path("."+m.Key, ".json.tmp")
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, s.path(m.Key, ".json")); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return s.evictLocked(time.Now())
}

// lock takes the store-wide flock that serialises writers and eviction.
func (s *Store) lock() (func(), error) {
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(s.Dir, ".lock"), os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}
	for {
		err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX)
		if !errors.Is(err, syscall.EINTR) {
			break
		}
	}
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("cache lock: %w", err)
	}
	return func() { _ = f.Close() }, nil
}
