package gitutil

import (
	"errors"
	"fmt"
)

// ErrDirty means the work tree differs from HEAD: a tracked file is modified,
// an untracked file is not ignored, or a submodule has changes. Such a tree is
// never cached, only identified (see WorkTree).
var ErrDirty = errors.New("work tree has uncommitted or untracked files")

// CleanTree returns the id of HEAD's effective tree (HEAD's tree without
// heavyq's own bookkeeping files, see effectiveTreeOf) when the work tree
// matches it exactly, else an error wrapping ErrDirty. Changes to the
// bookkeeping files alone (a ticked item, a new decision) do not make the
// work tree dirty. It is the identity the result cache keys on, and the id
// WorkTree reports for a clean tree.
func CleanTree(repo string) (string, error) {
	head, err := ResolveCommit(repo, "HEAD")
	if err != nil {
		return "", err
	}
	return cleanTreeAt(repo, head, bookkeepingPaths(repo, ""))
}

// cleanTreeAt is CleanTree for a HEAD already resolved to head. Every part of
// the answer refers to that commit: the tree comes from head, and if HEAD has
// moved by the time the status check is done, the answer is "not clean"
// instead of a mix of two commits.
func cleanTreeAt(repo, head string, exclude []string) (string, error) {
	clean, err := workTreeCleanExcept(repo, exclude)
	switch {
	case err != nil:
		return "", fmt.Errorf("%w (%v)", ErrDirty, err)
	case !clean:
		return "", ErrDirty
	}
	// git status trusts the index: it hides flagged entries and submodules
	// that are not checked out. Neither is HEAD's tree (see dirtyTreeAt).
	idx, err := scanIndex(repo, excludeSpecs(exclude))
	switch {
	case err != nil:
		return "", fmt.Errorf("%w (%v)", ErrDirty, err)
	case idx.hidden:
		return "", fmt.Errorf("%w (an index entry is assume-unchanged or skip-worktree)", ErrDirty)
	case len(idx.absent) > 0:
		return "", fmt.Errorf("%w (submodule %s is not checked out)", ErrDirty, idx.absent[0])
	}
	tree, err := effectiveTreeOf(repo, head, exclude)
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
	// ID is HEAD's effective tree id for a clean tree (what CleanTree
	// returns), and "<effective tree id>+<32 hex>" for a dirty one: the same edits give the same
	// id, any edit changes it, and undoing them restores the clean id.
	//
	// ID is "" (with Dirty set) when the tree cannot be identified reliably:
	// a submodule or nested repository with changes of its own, an index
	// entry flagged assume-unchanged or skip-worktree (sparse checkout flags
	// every path outside its cone), more untracked content than the
	// identification budget allows, or a HEAD that moved meanwhile. A
	// submodule that is not checked out gives a dirty id, never the clean one. An unknown tree must never be used as proof of anything.
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
	exclude := bookkeepingPaths(repo, "")
	tree, err := cleanTreeAt(repo, head, exclude)
	if err == nil {
		return TreeID{ID: tree}, nil
	}
	if !errors.Is(err, ErrDirty) {
		return TreeID{}, err
	}
	return dirtyTreeAt(repo, head, exclude)
}
