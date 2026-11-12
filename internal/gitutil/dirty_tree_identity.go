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
// The id is head's effective tree (without heavyq's bookkeeping files, see
// effectiveTreeOf) combined with a hash of `git diff <head> --binary`
// (every tracked change, staged or not) and a hash of the untracked files that
// are not ignored (names and contents). Ignored files are not part of the
// identity, consistent with the cache and with what a commit would contain.
// Edits to the bookkeeping paths (exclude) are not part of it either: the diff
// and the untracked listing skip them.
//
// Three cases are special:
//   - the edits cancel out (for example staged, then reverted by hand): the
//     files equal head's, so the clean id is returned;
//   - a submodule or nested repository has changes of its own: the diff only
//     says "dirty" for those, whatever they are, so the tree is unknown (ID "");
//   - too much untracked content to hash, or HEAD moved: unknown as well.
func dirtyTreeAt(repo, head string, exclude []string) (TreeID, error) {
	headTree, err := effectiveTreeOf(repo, head, exclude)
	if err != nil {
		return TreeID{}, err
	}
	return dirtyTreeFrom(repo, head, headTree, excludeSpecs(exclude), untrackedSpecs(exclude))
}

// dirtyTreeFrom is dirtyTreeAt on a head tree id already found (headTree, what a clean tree is called), with the
// paths the diff (diffSpecs) and the untracked listing (listSpecs) look at given as pathspecs after "--": a whole
// tree leaves out only the bookkeeping paths, a gate's scope (see IdentifyScoped) names the files that count.
func dirtyTreeFrom(repo, head, headTree string, diffSpecs, listSpecs []string) (TreeID, error) {
	unknown := TreeID{Dirty: true}
	sub, err := submoduleChanged(repo)
	if err != nil {
		return TreeID{}, err
	}
	if sub {
		return unknown, nil
	}
	diff, diffBytes, err := trackedDiffDigest(repo, head, diffSpecs)
	if err != nil {
		return TreeID{}, err
	}
	ut, err := untrackedDigestSpecs(repo, listSpecs)
	if err != nil {
		return TreeID{}, err
	}
	if headStill(repo, head) != nil || ut.unknown {
		return unknown, nil
	}
	if diffBytes == 0 && ut.files == 0 {
		return TreeID{ID: headTree}, nil
	}
	sum := sha256.Sum256([]byte("heavyq-tree-v2\x00" + diff + "\x00" + ut.digest))
	return TreeID{ID: headTree + "+" + hex.EncodeToString(sum[:])[:digestLen], Dirty: true}, nil
}

// diffArgs pins `git diff` against user configuration, so the same tree gives
// the same bytes under any git config: no external drivers or textconv, no
// rename detection, fixed prefixes, algorithm, heuristics and context, full
// hashes, submodules never ignored, and paths quoted the default way.
func diffArgs(head string, specs []string) []string {
	args := []string{"-c", "core.quotePath=true", "diff", head, "--binary", "--full-index", "--no-renames",
		"--no-ext-diff", "--no-textconv", "--no-color", "--src-prefix=a/", "--dst-prefix=b/",
		"--diff-algorithm=myers", "--no-indent-heuristic", "--submodule=short", "--ignore-submodules=none",
		"-U3", "--inter-hunk-context=0"}
	return append(args, specs...)
}

// trackedDiffDigest hashes `git diff <head> --binary`, streamed so a large
// binary change is never held in memory, and reports how many bytes it was.
func trackedDiffDigest(repo, head string, specs []string) (digest string, n int64, err error) {
	h := sha256.New()
	cw := &countWriter{w: h}
	if err := gitTo(cw, repo, diffArgs(head, specs)...); err != nil {
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
