// Package cache stores the output and exit code of commands keyed by the
// exact committed code they ran on, and coalesces identical in-flight runs.
package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"

	"github.com/LysCongThanh/heavyq/internal/gitutil"
	"github.com/LysCongThanh/heavyq/internal/runident"
)

// KeyInput is everything that identifies a cacheable run.
type KeyInput struct {
	Dir      string   // where the command runs
	Argv     []string // exact argv
	EnvNames []string // allowlisted variables whose values are part of the key
	Version  string   // heavyq version
	BaseRef  string   // --base-ref (remote/branch): its commit joins the key
	Siblings []string // pinned --with repositories as "name@sha": they join the key
	Getenv   func(string) (string, bool)
}

// keyDoc is hashed as canonical JSON. The run itself is identified by its
// run_id (internal/runident, the one definition of "the same run"); the
// heavyq version is added because a new version may store results differently.
// Changing keyDoc invalidates old entries, which is safe (they simply miss):
// schema 2 replaced the schema 1 fields by the run_id, schema 3 came with run
// identity schema 2 (the tree id leaves out heavyq's own bookkeeping files).
type keyDoc struct {
	Schema  int    `json:"schema"`
	RunID   string `json:"run_id"`
	Version string `json:"version"`
}

// Key returns the cache key of a run, or "" and the reason the run cannot
// be cached (not in a repo, or the work tree differs from HEAD).
func Key(in KeyInput) (string, string) {
	key, _, why := KeyAndTree(in)
	return key, why
}

// KeyAndTree is Key plus the tree id the key was computed on (keyDoc.Tree).
// A result stored or replayed under the key proves that tree, whatever the
// work tree holds by the time the result is used.
func KeyAndTree(in KeyInput) (key, tree, why string) {
	top, err := gitutil.TopLevel(in.Dir)
	if err != nil {
		return "", "", "not in a git repository"
	}
	// The same identity the ledger records for a clean tree (gitutil.WorkTree);
	// a dirty tree has an identity too, but it is never cached.
	tree, err = gitutil.CleanTree(top)
	if errors.Is(err, gitutil.ErrDirty) {
		return "", "", "work tree has uncommitted or untracked files"
	}
	if err != nil {
		return "", "", "no HEAD commit"
	}
	sub, err := gitutil.Prefix(in.Dir)
	if err != nil {
		return "", "", "cannot resolve the subdirectory"
	}
	id := runident.ID(runident.Input{Tree: tree, Argv: in.Argv, Subdir: sub, EnvNames: in.EnvNames,
		Getenv: in.Getenv, BaseSHA: runident.BaseSHA(top, in.BaseRef), Siblings: in.Siblings})
	b, err := json.Marshal(keyDoc{Schema: 3, RunID: id, Version: in.Version})
	if err != nil {
		return "", "", "cannot encode the key"
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), tree, ""
}
