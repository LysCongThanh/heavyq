package stance

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Trust is the list of repositories whose own stances (.heavyq/stances) the
// user has read and allowed, the `direnv allow` model: a stance file from a
// repository is text that gets injected into an agent's context, so a
// repository you just cloned must not be able to do that on its own.
//
// An entry is a content hash of the repository's stance files, per repository
// (its git common dir). Any edit, addition or removal changes the hash and
// revokes the trust until `heavyq trust` is run again. Built-in stances and the
// user's own (~/.config/heavyq/stances) are always trusted.
type Trust struct {
	// Path is the trust file, in the user's state directory (0600).
	Path string
}

// TrustEntry is one trusted repository.
type TrustEntry struct {
	Hash string    `json:"hash"`
	At   time.Time `json:"at"`
}

type trustFile struct {
	V     int                   `json:"v"`
	Repos map[string]TrustEntry `json:"repos"`
}

// ContentHash returns the hash of the stance files in dir (the repo's
// .heavyq/stances) and their names, sorted. It covers the files the loader
// reads (regular, non-dot "*.md"): names and bytes, so a rename counts. A
// missing or empty directory has no files and the empty hash.
func ContentHash(dir string) (hash string, names []string, err error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil, nil
	}
	if err != nil {
		return "", nil, err
	}
	h := sha256.New()
	for _, e := range entries { // ReadDir sorts by name
		name := e.Name()
		if strings.HasPrefix(name, ".") || !strings.HasSuffix(name, ".md") || e.IsDir() {
			continue
		}
		data, err := readBounded(filepath.Join(dir, name))
		if err != nil {
			return "", nil, fmt.Errorf("%s: %w", filepath.Join(dir, name), err)
		}
		fmt.Fprintf(h, "%s\x00%d\x00", name, len(data))
		h.Write(data)
		names = append(names, name)
	}
	if len(names) == 0 {
		return "", nil, nil
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), names, nil
}

// Entries returns every trusted repository, by its common dir.
func (t Trust) Entries() (map[string]TrustEntry, error) {
	data, err := os.ReadFile(t.Path)
	if errors.Is(err, fs.ErrNotExist) {
		return map[string]TrustEntry{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("trust file: %w", err)
	}
	var f trustFile
	if err := json.Unmarshal(data, &f); err != nil {
		// A damaged file trusts nothing: fail closed.
		return map[string]TrustEntry{}, fmt.Errorf("trust file %s is damaged: %w", t.Path, err)
	}
	if f.Repos == nil {
		f.Repos = map[string]TrustEntry{}
	}
	return f.Repos, nil
}

// Trusted says whether the repo is trusted with exactly this content hash. An
// unreadable or damaged trust file, an unknown repo and a changed hash all
// say no.
func (t Trust) Trusted(repo, hash string) bool {
	if hash == "" {
		return false
	}
	entries, err := t.Entries()
	return err == nil && entries[repo].Hash == hash
}

// Grant records hash as trusted for repo.
func (t Trust) Grant(repo, hash string, now time.Time) error {
	return t.update(func(m map[string]TrustEntry) { m[repo] = TrustEntry{Hash: hash, At: now.UTC()} })
}

// Revoke forgets repo and reports whether it was trusted.
func (t Trust) Revoke(repo string) (bool, error) {
	was := false
	err := t.update(func(m map[string]TrustEntry) { _, was = m[repo]; delete(m, repo) })
	return was, err
}

func (t Trust) update(fn func(map[string]TrustEntry)) error {
	m, err := t.Entries()
	if err != nil { // damaged: starting over only ever trusts less
		m = map[string]TrustEntry{}
	}
	fn(m)
	data, err := json.MarshalIndent(trustFile{V: 1, Repos: m}, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(t.Path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(t.Path), ".trust-*")
	if err != nil {
		return err
	}
	_, werr := tmp.Write(append(data, '\n'))
	if err := errors.Join(werr, tmp.Close()); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	if err := os.Rename(tmp.Name(), t.Path); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return nil
}

// SortedRepos returns the keys of entries in order.
func SortedRepos(entries map[string]TrustEntry) []string {
	keys := make([]string, 0, len(entries))
	for k := range entries {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
