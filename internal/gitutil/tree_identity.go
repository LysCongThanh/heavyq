package gitutil

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
)

// ErrDirty means the work tree differs from HEAD: a tracked file is modified
// or an untracked file is not ignored. Such a tree is never cached, only
// identified (see WorkTree).
var ErrDirty = errors.New("work tree has uncommitted or untracked files")

// CleanTree returns the id of HEAD's tree when the work tree matches it
// exactly, else an error wrapping ErrDirty. It is the identity the result
// cache keys on, and the id WorkTree reports for a clean tree.
func CleanTree(repo string) (string, error) {
	clean, err := workTreeClean(repo)
	switch {
	case err != nil:
		return "", fmt.Errorf("%w (%v)", ErrDirty, err)
	case !clean:
		return "", ErrDirty
	}
	return TreeOf(repo, "HEAD")
}

// TreeID identifies the exact files of a work tree.
type TreeID struct {
	// ID is HEAD's tree id for a clean tree (what CleanTree returns), and
	// "<HEAD tree id>+<32 hex>" for a dirty one: the same edits give the same
	// id, any edit changes it, and undoing them restores the clean id.
	ID    string
	Dirty bool
}

// digestLen is how many hex characters of the edits' digest join a dirty id.
const digestLen = 32

// WorkTree identifies the work tree rooted at repo (a path from TopLevel).
//
// A dirty id is HEAD's tree combined with a hash of `git diff HEAD --binary`
// (every tracked change, staged or not) and a hash of the untracked files that
// are not ignored (names and contents). Ignored files are not part of the
// identity, consistent with the cache and with what a commit would contain.
func WorkTree(repo string) (TreeID, error) {
	tree, err := CleanTree(repo)
	if err == nil {
		return TreeID{ID: tree}, nil
	}
	if !errors.Is(err, ErrDirty) {
		return TreeID{}, err
	}
	head, err := TreeOf(repo, "HEAD")
	if err != nil {
		return TreeID{}, err
	}
	diff, err := trackedDiffDigest(repo)
	if err != nil {
		return TreeID{}, err
	}
	untracked, err := untrackedDigest(repo)
	if err != nil {
		return TreeID{}, err
	}
	sum := sha256.Sum256([]byte("heavyq-tree-v1\x00" + diff + "\x00" + untracked))
	return TreeID{ID: head + "+" + hex.EncodeToString(sum[:])[:digestLen], Dirty: true}, nil
}

// trackedDiffDigest hashes `git diff HEAD --binary`, streamed so a large
// binary change is never held in memory. The flags pin the output against
// user configuration (external diff drivers, renames, prefixes, abbreviation).
func trackedDiffDigest(repo string) (string, error) {
	h := sha256.New()
	err := gitTo(h, repo, "diff", "HEAD", "--binary", "--full-index", "--no-renames",
		"--no-ext-diff", "--no-textconv", "--no-color", "--src-prefix=a/", "--dst-prefix=b/")
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
