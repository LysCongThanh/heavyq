package gitutil

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strings"
)

// Checkout says where a command ran and on which code. Every field is empty
// outside a git repository.
type Checkout struct {
	Repo     string // hash of the absolute git common dir: shared by all work trees of a repo
	Worktree string // absolute path of the work tree root
	Subdir   string // the directory relative to Worktree ("" at the root)
	Commit   string // HEAD ("" before the first commit)
	Tree     string // exact files: see TreeID.ID
	Dirty    bool
}

// repoIDLen is how many hex characters of the common dir hash name a repo.
const repoIDLen = 16

// Locate fills Repo, Worktree and Subdir for dir.
func Locate(dir string) (Checkout, error) {
	out, err := gitRaw(dir, "rev-parse", "--show-toplevel", "--git-common-dir", "--show-prefix")
	if err != nil {
		return Checkout{}, err
	}
	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	if len(lines) != 3 || lines[0] == "" || lines[1] == "" {
		return Checkout{}, fmt.Errorf("%s: unexpected rev-parse output %q", dir, out)
	}
	common := lines[1]
	if !filepath.IsAbs(common) {
		common = filepath.Join(dir, common) // relative to git's working directory
	}
	if abs, err := filepath.Abs(common); err == nil {
		common = abs
	}
	if real, err := filepath.EvalSymlinks(common); err == nil {
		common = real
	}
	sum := sha256.Sum256([]byte(common))
	return Checkout{
		Repo:     hex.EncodeToString(sum[:])[:repoIDLen],
		Worktree: lines[0],
		Subdir:   strings.TrimSuffix(lines[2], "/"),
	}, nil
}

// Identify is Locate plus the commit and the exact tree the work tree holds
// right now, dirty or not. A repository without commits has no Commit or Tree.
// On error the fields found so far are still returned.
func Identify(dir string) (Checkout, error) {
	co, err := Locate(dir)
	if err != nil {
		return co, err
	}
	if co.Commit, err = ResolveCommit(co.Worktree, "HEAD"); err != nil {
		co.Commit, co.Dirty = "", !IsClean(co.Worktree)
		return co, nil // unborn HEAD: a repo, but nothing to identify yet
	}
	id, err := WorkTree(co.Worktree)
	if err != nil {
		return co, err
	}
	co.Tree, co.Dirty = id.ID, id.Dirty
	return co, nil
}

// Pinned is Locate for a run of commit sha (a remote run ships exactly that
// commit): Commit is sha, Tree is its tree and the checkout is never dirty,
// whatever the local work tree looks like.
func Pinned(dir, sha string) (Checkout, error) {
	co, err := Locate(dir)
	if err != nil {
		return co, err
	}
	if co.Commit, err = ResolveCommit(co.Worktree, sha); err != nil {
		return co, err
	}
	co.Tree, err = TreeOf(co.Worktree, co.Commit)
	return co, err
}
