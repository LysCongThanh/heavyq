package gitutil

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// digestLen is how many hex characters of the edits' digest join a dirty id.
const digestLen = 32

// gitlinkMode is the index mode of a submodule (or an embedded repository
// that was added by mistake).
const gitlinkMode = "160000"

// dirtyTreeAt identifies a work tree that differs from head.
//
// The id is head's tree combined with a hash of `git diff <head> --binary`
// (every tracked change, staged or not) and a hash of the untracked files that
// are not ignored (names and contents). Ignored files are not part of the
// identity, consistent with the cache and with what a commit would contain.
//
// Three cases are special:
//   - the edits cancel out (for example staged, then reverted by hand): the
//     files equal head's, so the clean id is returned;
//   - a submodule or nested repository has changes of its own: the diff only
//     says "dirty" for those, whatever they are, so the tree is unknown (ID "");
//   - too much untracked content to hash, or HEAD moved: unknown as well.
func dirtyTreeAt(repo, head string) (TreeID, error) {
	unknown := TreeID{Dirty: true}
	headTree, err := TreeOf(repo, head)
	if err != nil {
		return TreeID{}, err
	}
	sub, err := submoduleChanged(repo)
	if err != nil {
		return TreeID{}, err
	}
	if sub {
		return unknown, nil
	}
	diff, diffBytes, err := trackedDiffDigest(repo, head)
	if err != nil {
		return TreeID{}, err
	}
	ut, err := untrackedDigest(repo)
	if err != nil {
		return TreeID{}, err
	}
	if headStill(repo, head) != nil || ut.unknown {
		return unknown, nil
	}
	if diffBytes == 0 && ut.files == 0 {
		return TreeID{ID: headTree}, nil
	}
	sum := sha256.Sum256([]byte("heavyq-tree-v1\x00" + diff + "\x00" + ut.digest))
	return TreeID{ID: headTree + "+" + hex.EncodeToString(sum[:])[:digestLen], Dirty: true}, nil
}

// diffArgs pins `git diff` against user configuration, so the same tree gives
// the same bytes under any git config: no external drivers or textconv, no
// rename detection, fixed prefixes, algorithm, heuristics and context, full
// hashes, submodules never ignored, and paths quoted the default way.
func diffArgs(head string) []string {
	return []string{"-c", "core.quotePath=true", "diff", head, "--binary", "--full-index", "--no-renames",
		"--no-ext-diff", "--no-textconv", "--no-color", "--src-prefix=a/", "--dst-prefix=b/",
		"--diff-algorithm=myers", "--no-indent-heuristic", "--submodule=short", "--ignore-submodules=none",
		"-U3", "--inter-hunk-context=0"}
}

// trackedDiffDigest hashes `git diff <head> --binary`, streamed so a large
// binary change is never held in memory, and reports how many bytes it was.
func trackedDiffDigest(repo, head string) (digest string, n int64, err error) {
	h := sha256.New()
	cw := &countWriter{w: h}
	if err := gitTo(cw, repo, diffArgs(head)...); err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), cw.n, nil
}

type countWriter struct {
	w io.Writer
	n int64
}

func (c *countWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n += int64(n)
	return n, err
}

// submoduleChanged reports whether any checked-out submodule (or embedded
// repository recorded in the index) has uncommitted or untracked files of its
// own. The superproject's diff shows only "-dirty" for these, the same for
// every kind of change, so such a tree has no reliable id. A submodule that
// moved to another commit is not in this set: the diff names the commit.
func submoduleChanged(repo string) (bool, error) {
	list, err := gitRaw(repo, "ls-files", "--stage", "-z")
	if err != nil {
		return false, err
	}
	for _, entry := range strings.Split(list, "\x00") {
		meta, path, ok := strings.Cut(entry, "\t")
		if !ok || !strings.HasPrefix(meta, gitlinkMode+" ") {
			continue
		}
		dir := filepath.Join(repo, filepath.FromSlash(path))
		if _, err := os.Lstat(filepath.Join(dir, ".git")); err != nil {
			continue // not checked out: nothing in it can be edited
		}
		out, err := gitRaw(dir, "status", "--porcelain", "--untracked-files=normal", "--ignore-submodules=none")
		if err != nil || out != "" {
			return true, nil // unreadable counts as changed: never claim an id
		}
	}
	return false, nil
}
