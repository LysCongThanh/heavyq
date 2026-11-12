package gitutil

import (
	"os"
	"path/filepath"
	"strings"
)

// indexScan is what one read of the index tells beyond `git status` and
// `git diff`, which both trust the index in two ways that hide files:
//
//   - an entry flagged assume-unchanged or skip-worktree (set by
//     `git update-index`, and by sparse checkout on every path outside its
//     cone) is taken to match HEAD, whatever the work tree holds or lacks;
//   - a submodule that is not checked out (`git worktree add` never checks
//     them out) is reported as unchanged, though none of its files are there.
type indexScan struct {
	// hidden: some entry is flagged, so the files may differ from HEAD in
	// ways no git command reports. Such a tree has no reliable id.
	hidden bool
	// checkedOut are the gitlinks whose directory holds a repository.
	checkedOut []string
	// absent are the gitlinks whose directory holds none: their files are
	// missing from the work tree. They are part of a dirty id.
	absent []string
}

// scanIndex reads the index of the work tree at repo once, with
// `git ls-files --stage -t -v`: each entry is "<tag> <mode> <id> <stage>\t<path>",
// where the tag is "S" for skip-worktree and lower case for assume-unchanged.
// Only entries that specs (pathspecs after "--") let through are read: the
// bookkeeping paths are never part of the identity, and a gate's scope (see
// IdentifyScoped) names the files that count, so a flag elsewhere hides
// nothing that counts.
func scanIndex(repo string, specs []string) (indexScan, error) {
	args := append([]string{"ls-files", "--stage", "-t", "-v", "-z"}, specs...)
	list, err := gitRaw(repo, args...)
	if err != nil {
		return indexScan{}, err
	}
	var s indexScan
	for _, entry := range strings.Split(list, "\x00") {
		meta, path, ok := strings.Cut(entry, "\t")
		tag, rest, spaced := strings.Cut(meta, " ")
		if !ok || !spaced || tag == "" {
			continue
		}
		if tag == "S" || strings.ToUpper(tag) != tag {
			s.hidden = true
		}
		if !strings.HasPrefix(rest, gitlinkMode+" ") {
			continue
		}
		if _, err := os.Lstat(filepath.Join(repo, filepath.FromSlash(path), ".git")); err != nil {
			s.absent = append(s.absent, path)
		} else {
			s.checkedOut = append(s.checkedOut, path)
		}
	}
	return s, nil
}

// submoduleChanged reports whether any checked-out submodule (or embedded
// repository recorded in the index) has uncommitted or untracked files of its
// own. The superproject's diff shows only "-dirty" for these, the same for
// every kind of change, so such a tree has no reliable id. A submodule that
// moved to another commit is not in this set: the diff names the commit.
func submoduleChanged(repo string, checkedOut []string) bool {
	for _, path := range checkedOut {
		dir := filepath.Join(repo, filepath.FromSlash(path))
		out, err := gitRaw(dir, "status", "--porcelain", "--untracked-files=normal", "--ignore-submodules=none")
		if err != nil || out != "" {
			return true // unreadable counts as changed: never claim an id
		}
	}
	return false
}
