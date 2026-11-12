package toolcheck

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/LysCongThanh/heavyq/internal/config"
)

// How long an answer is believed. A tool that is there stays there; a missing one may be installed, or its
// directory added to env_path, any minute (and env_path is part of the key, so changing it asks again).
const (
	FoundTTL   = time.Hour
	MissingTTL = 2 * time.Minute
	// maxCacheBytes bounds what is read back; a file holds a few dozen short entries.
	maxCacheBytes = 64 << 10
)

// Answer is what a remote said about one tool: found, where, and when it was asked.
type Answer struct {
	Found bool      `json:"found"`
	Path  string    `json:"path,omitempty"`
	At    time.Time `json:"at"`
}

func (a Answer) fresh(now time.Time) bool {
	ttl := MissingTTL
	if a.Found {
		ttl = FoundTTL
	}
	age := now.Sub(a.At)
	return age >= 0 && age < ttl
}

// Answers converts a probe's result (tool -> path, "" when not found) asked at at.
func Answers(paths map[string]string, at time.Time) map[string]Answer {
	out := make(map[string]Answer, len(paths))
	for t, p := range paths {
		out[t] = Answer{Found: p != "", Path: p, At: at}
	}
	return out
}

// Key identifies the PATH a remote's jobs get: its env_path and transport. Answers stored under another
// key do not count.
func Key(envPath string, transport []string) string {
	sum := sha256.Sum256([]byte(envPath + "\x00" + strings.Join(transport, "\x00")))
	return hex.EncodeToString(sum[:8])
}

// Cache keeps the answers of each remote in Dir/<remote>.json; with Dir "" it keeps nothing. A file that
// cannot be read is a miss, and writes replace the file atomically: the worst a broken or concurrent file
// costs is one more probe.
type Cache struct{ Dir string }

type cacheFile struct {
	Key   string            `json:"key"`
	Tools map[string]Answer `json:"tools"`
}

func (c Cache) path(remote string) (string, error) {
	if !config.SafeName.MatchString(remote) {
		return "", fmt.Errorf("remote name %q is not a safe file name", remote)
	}
	return filepath.Join(c.Dir, remote+".json"), nil
}

func (c Cache) read(remote string) cacheFile {
	p, err := c.path(remote)
	if c.Dir == "" || err != nil {
		return cacheFile{}
	}
	b, err := os.ReadFile(p)
	var f cacheFile
	if err != nil || len(b) > maxCacheBytes || json.Unmarshal(b, &f) != nil {
		return cacheFile{}
	}
	return f
}

// Lookup returns the fresh answers about tools that remote gave under key, and the tools still to ask.
func (c Cache) Lookup(remote, key string, tools []string, now time.Time) (known map[string]Answer, stale []string) {
	f := c.read(remote)
	known = map[string]Answer{}
	for _, t := range tools {
		if a, ok := f.Tools[t]; ok && f.Key == key && a.fresh(now) {
			known[t] = a
		} else {
			stale = append(stale, t)
		}
	}
	return known, stale
}

// Store merges answers into what is kept for remote under key, dropping answers too old to be used.
func (c Cache) Store(remote, key string, answers map[string]Answer) error {
	if c.Dir == "" {
		return nil
	}
	p, err := c.path(remote)
	if err != nil {
		return err
	}
	f := c.read(remote)
	if f.Key != key || f.Tools == nil {
		f = cacheFile{Key: key, Tools: map[string]Answer{}}
	}
	var latest time.Time // the time of the newest answer: what "too old" is measured from
	for _, a := range answers {
		if a.At.After(latest) {
			latest = a.At
		}
	}
	for t, a := range f.Tools {
		if latest.Sub(a.At) >= FoundTTL {
			delete(f.Tools, t)
		}
	}
	for t, a := range answers {
		f.Tools[t] = a
	}
	b, err := json.Marshal(f)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(c.Dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(c.Dir, "."+remote+".*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // a no-op once renamed
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), p)
}

// Forget drops everything kept for remote: after a command was not found there, the next run asks again.
func (c Cache) Forget(remote string) error {
	p, err := c.path(remote)
	if c.Dir == "" || err != nil {
		return err
	}
	if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}
