package gitutil

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/LysCongThanh/heavyq/internal/gatepaths"
)

// ScopedIDPrefix starts the id of a scoped tree (see IdentifyScoped); a whole tree id is plain hex.
const ScopedIDPrefix = "paths-"

// ErrScopeEmpty means the paths of a gate match no file of the commit: such a gate would be proven by any code,
// so its code has no identity at all.
var ErrScopeEmpty = errors.New("the gate's paths match no file at HEAD, so there is no code to prove")

// Object ids of the empty tree: git knows it without storing it, for both object formats.
const (
	emptyTreeSHA1   = "4b825dc642cb6eb9a060e54bf8d69288fbee4904"
	emptyTreeSHA256 = "6ef19b41225c5369f1c104d45d8d85efa9b057b53b14b4b9b939dd74decc5321"
)

// IdentifyScoped is Identify for a gate that declares `paths` (see package gatepaths): Tree and Dirty describe
// only the files those patterns name, so a commit or an edit that touches none of them leaves the identity, and
// with it the gate's proof, as it was. No paths is Identify, the whole tree.
//
// The scoped id is "paths-" and 40 hex characters of a digest of the patterns and of the committed files they
// match (their modes, object ids and names), plus "+" and the digest of the edits to those files when the work
// tree has any, like a whole tree id. It never looks like a git tree id, so a scoped and a whole id cannot meet.
// Changing the patterns changes the id. Bookkeeping files (see bookkeepingPaths) never count. Patterns that
// match no committed file give ErrScopeEmpty, and so do invalid patterns: unknown input fails closed.
func IdentifyScoped(dir string, paths []string) (Checkout, error) {
	if len(paths) == 0 {
		return Identify(dir)
	}
	return identify(dir, func(repo, head string) (TreeID, error) {
		return scopedWorkTreeAt(repo, head, paths, bookkeepingPaths(repo, ""))
	})
}

// PinnedScoped is Pinned for a gate's scope: the scoped id of commit sha, never dirty.
func PinnedScoped(dir, sha string, paths []string) (Checkout, error) {
	if len(paths) == 0 {
		return Pinned(dir, sha)
	}
	return pinned(dir, sha, func(repo, commit string) (string, error) {
		return scopedCommittedID(repo, commit, paths, bookkeepingPaths(repo, commit))
	})
}

// scopeSpecs are the pathspecs (after "--") that let through only the files matching paths, less the bookkeeping
// paths and nested work trees. "top" and "glob" make each pattern root-relative with git's own glob rules, the
// same wherever git runs and for every command (diff, ls-files, status, diff-tree).
func scopeSpecs(paths, exclude []string) []string {
	specs := []string{"--"}
	for _, p := range paths {
		specs = append(specs, ":(top,glob)"+p)
	}
	for _, p := range append(append([]string{}, exclude...), NestedWorkTreeDirs...) {
		specs = append(specs, ":(top,exclude,literal)"+p)
	}
	return specs
}

// scopedCommittedID is the scoped id of commit: its files that match paths, listed by git as the additions that
// turn the empty tree into commit (mode, object id, name; git's own order), hashed with the patterns.
func scopedCommittedID(repo, commit string, paths, exclude []string) (string, error) {
	if err := gatepaths.ValidList(paths); err != nil {
		return "", fmt.Errorf("gate paths: %w", err)
	}
	empty := emptyTreeSHA1
	if len(commit) == 64 {
		empty = emptyTreeSHA256
	}
	h := sha256.New()
	canon := gatepaths.Canonical(paths)
	fmt.Fprint(h, "heavyq-scope-v1\x00", strings.Join(canon, "\x00"), "\x00\x00") // hash.Hash.Write never fails
	cw := &countWriter{w: h}
	args := append([]string{"diff-tree", "-r", "-z", "--raw", "--full-index", "--no-renames", empty, commit}, scopeSpecs(canon, exclude)...)
	if err := gitTo(cw, repo, args...); err != nil {
		return "", err
	}
	if cw.n == 0 {
		return "", ErrScopeEmpty
	}
	return ScopedIDPrefix + hex.EncodeToString(h.Sum(nil))[:40], nil
}

// scopedWorkTreeAt identifies the files of the work tree at repo that match paths, against head. A work tree
// whose edits are all outside the scope is clean for it, and has the commit's scoped id.
func scopedWorkTreeAt(repo, head string, paths, exclude []string) (TreeID, error) {
	id, err := scopedCommittedID(repo, head, paths, exclude)
	if err != nil {
		return TreeID{}, err
	}
	specs := scopeSpecs(gatepaths.Canonical(paths), exclude)
	clean, err := workTreeCleanSpecs(repo, specs)
	if err == nil && clean {
		if headStill(repo, head) != nil {
			return TreeID{Dirty: true}, nil
		}
		return TreeID{ID: id}, nil
	}
	return dirtyTreeFrom(repo, head, id, specs, specs)
}
