package gitutil

import (
	"errors"
	"fmt"
)

// ErrDirty means the work tree differs from HEAD: a tracked file is modified,
// an untracked file is not ignored, or a submodule has changes. Such a tree is
// never cached, only identified (see WorkTree).
var ErrDirty = errors.New("work tree has uncommitted or untracked files")

// CleanTree returns the id of HEAD's tree when the work tree matches it
// exactly, else an error wrapping ErrDirty. It is the identity the result
// cache keys on, and the id WorkTree reports for a clean tree.
func CleanTree(repo string) (string, error) {
	head, err := ResolveCommit(repo, "HEAD")
	if err != nil {
		return "", err
	}
	return cleanTreeAt(repo, head)
}

// cleanTreeAt is CleanTree for a HEAD already resolved to head. Every part of
// the answer refers to that commit: the tree comes from head, and if HEAD has
// moved by the time the status check is done, the answer is "not clean"
// instead of a mix of two commits.
func cleanTreeAt(repo, head string) (string, error) {
	clean, err := workTreeClean(repo)
	switch {
	case err != nil:
		return "", fmt.Errorf("%w (%v)", ErrDirty, err)
	case !clean:
		return "", ErrDirty
	}
	tree, err := TreeOf(repo, head)
	if err != nil {
		return "", err
	}
	if err := headStill(repo, head); err != nil {
		return "", fmt.Errorf("%w (%v)", ErrDirty, err)
	}
	return tree, nil
}

// headStill fails when HEAD no longer resolves to head: someone committed,
// reset or switched branches while the tree was being identified.
func headStill(repo, head string) error {
	if now, err := ResolveCommit(repo, "HEAD"); err != nil || now != head {
		return errors.New("HEAD moved while identifying the tree")
	}
	return nil
}

// TreeID identifies the exact files of a work tree.
type TreeID struct {
	// ID is HEAD's tree id for a clean tree (what CleanTree returns), and
	// "<HEAD tree id>+<32 hex>" for a dirty one: the same edits give the same
	// id, any edit changes it, and undoing them restores the clean id.
	//
	// ID is "" (with Dirty set) when the tree cannot be identified reliably:
	// a submodule or nested repository with changes of its own, more untracked
	// content than the identification budget allows, or a HEAD that moved
	// meanwhile. An unknown tree must never be used as proof of anything.
	ID    string
	Dirty bool
}

// WorkTree identifies the work tree rooted at repo (a path from TopLevel).
// HEAD is resolved once; see dirtyTreeAt for what a dirty id covers.
func WorkTree(repo string) (TreeID, error) {
	head, err := ResolveCommit(repo, "HEAD")
	if err != nil {
		return TreeID{}, err
	}
	return workTreeAt(repo, head)
}

func workTreeAt(repo, head string) (TreeID, error) {
	tree, err := cleanTreeAt(repo, head)
	if err == nil {
		return TreeID{ID: tree}, nil
	}
	if !errors.Is(err, ErrDirty) {
		return TreeID{}, err
	}
	return dirtyTreeAt(repo, head)
}
