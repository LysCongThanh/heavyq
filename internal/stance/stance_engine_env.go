package stance

import (
	"errors"
	"path/filepath"
	"time"

	"github.com/LysCongThanh/heavyq/internal/gitutil"
	"github.com/LysCongThanh/heavyq/internal/history"
	"github.com/LysCongThanh/heavyq/internal/lease"
)

// Env is everything outside the process that stance operations read: where the
// stances, the trust list and the ledger are, and the clock. Tests build one
// over temporary directories; DefaultEnv is the real one.
type Env struct {
	Dirs   Dirs
	Trust  Trust
	Ledger string // the ledger path (history.DefaultPath)
	Now    func() time.Time
}

// TrustPath is where the trust list lives: beside the ledger, in the user's
// state directory.
func TrustPath(ledger string) string {
	if ledger == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(ledger), "stance-trust.json")
}

// DefaultEnv is the Env of a process running in cwd.
func DefaultEnv(cwd string) Env {
	ledger := history.DefaultPath()
	return Env{Dirs: DefaultDirs(cwd), Trust: Trust{Path: TrustPath(ledger)}, Ledger: ledger, Now: time.Now}
}

func (e Env) now() time.Time {
	if e.Now != nil {
		return e.Now()
	}
	return time.Now()
}

// ErrNoRepo is returned where stance state is needed outside a git repository:
// it lives in the git common dir.
var ErrNoRepo = errors.New("stance: not inside a git repository")

// Set is the stances in effect for a directory.
type Set struct {
	// All is every stance by precedence (Load); `stance ls` and `show` use it.
	All []Stance
	// Warnings are the "*.md" files of the stance directories that are not
	// stances and were ignored (`stance ls` lists them).
	Warnings []Warning
	// Injectable is All without the repo's stances when the repo is not
	// trusted: the only stances that may enter a lane or be injected.
	Injectable []Stance
	// RepoTrusted: the repo's stance files are trusted, or there are none.
	RepoTrusted bool
	// RepoHash is the content hash of the repo's stance files ("" when none).
	RepoHash string
	// Common is the git common dir ("" outside a repository), the key of the
	// trust list and the place of the lane state.
	Common string
}

// Untrusted reports whether s comes from a repo whose stances are not trusted.
func (set Set) Untrusted(s Stance) bool { return s.Source == SourceRepo && !set.RepoTrusted }

// LoadSet loads the stances for cwd and decides which may be injected. Invalid
// stance files are an error, as in Load.
func (e Env) LoadSet(cwd string) (Set, error) {
	all, warns, err := LoadReport(e.Dirs)
	if err != nil {
		return Set{}, err
	}
	set := Set{All: all, Injectable: all, RepoTrusted: true, Warnings: warns}
	if common, err := gitutil.CommonDir(cwd); err == nil {
		set.Common = common
	}
	if e.Dirs.Repo == "" {
		return set, nil
	}
	hash, _, err := ContentHash(e.Dirs.Repo)
	if err != nil {
		return Set{}, err
	}
	set.RepoHash = hash
	if hash == "" || (set.Common != "" && e.Trust.Trusted(set.Common, hash)) {
		return set, nil
	}
	set.RepoTrusted = false
	if set.Injectable, _, err = loadLayers(e.Dirs, false); err != nil {
		return Set{}, err
	}
	return set, nil
}

// OpenStoreFor opens the lane store of the repository of set.
func (set Set) OpenStoreFor() (*Store, error) {
	if set.Common == "" {
		return nil, ErrNoRepo
	}
	return OpenStore(set.Common)
}

// LaneItem is the item the lane holds a live lease on, "" when none.
func LaneItem(common, lane string, now time.Time) string {
	ls, err := lease.Open(common)
	if err != nil {
		return ""
	}
	held, err := ls.List()
	if err != nil {
		return ""
	}
	for _, l := range held {
		if l.Lane == lane && !l.Stale && now.Before(l.Expires) {
			return l.Item
		}
	}
	return ""
}

// workTreeTop is the root of the work tree around cwd.
func workTreeTop(cwd string) (string, error) {
	top, err := gitutil.TopLevel(cwd)
	if err != nil {
		return "", ErrNoRepo
	}
	return top, nil
}
