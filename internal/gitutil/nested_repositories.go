package gitutil

import (
	"os"
	"path/filepath"
	"strings"
)

// NestedWorkTreeDirs are directories that hold other checkouts of the
// repository by convention: Claude Code puts the work trees of its agents in
// .claude/worktrees inside the repository. They are never part of the code of
// the work tree around them, so no tree identity looks inside, whatever is in
// .gitignore. `heavyq doctor` and `heavyq init` suggest ignoring them for git
// too.
var NestedWorkTreeDirs = []string{".claude/worktrees"}

// untrackedSpecs are the pathspecs that leave exclude (the bookkeeping paths)
// and NestedWorkTreeDirs out of a listing of untracked files.
func untrackedSpecs(exclude []string) []string {
	return excludeSpecs(append(append([]string{}, exclude...), NestedWorkTreeDirs...))
}

// isNestedRepo is true for an entry of `git ls-files --others` or `git status`
// that is a directory holding its own .git (a file, for a linked work tree of
// this or another repository; a directory, for a clone). git lists such a
// directory as one opaque "dir/" entry: it belongs to another repository, so
// its files are not files of this work tree and its edits do not change it.
func isNestedRepo(repo, entry string) bool {
	if !strings.HasSuffix(entry, "/") {
		return false
	}
	_, err := os.Lstat(filepath.Join(repo, filepath.FromSlash(entry), ".git"))
	return err == nil
}

// onlyNestedRepos reports whether every untracked entry below dir (a "dir/"
// entry of `git status`, which collapses untracked directories) is a nested
// repository, so the directory holds no untracked file of this work tree.
func onlyNestedRepos(repo, dir string, exclude []string) (bool, error) {
	args := append([]string{"ls-files", "--others", "--exclude-standard", "-z"}, untrackedSpecs(exclude)...)
	args = append(args, ":(top,literal)"+dir)
	list, err := gitRaw(repo, args...)
	if err != nil {
		return false, err
	}
	for _, name := range strings.Split(list, "\x00") {
		if name != "" && !isNestedRepo(repo, name) {
			return false, nil
		}
	}
	return true, nil
}

// UnignoredNestedDirs returns the NestedWorkTreeDirs that exist in the work
// tree rooted at repo and that git does not ignore: `git status` lists their
// contents as untracked. Tree identity leaves them out either way; the list is
// for the hint that suggests ignoring them (`heavyq doctor`, `heavyq init`).
func UnignoredNestedDirs(repo string) []string {
	var out []string
	for _, d := range NestedWorkTreeDirs {
		if fi, err := os.Stat(filepath.Join(repo, filepath.FromSlash(d))); err != nil || !fi.IsDir() {
			continue
		}
		// check-ignore exits 1 for "not ignored"; the probe name only has to sit below d.
		if _, err := gitRaw(repo, "check-ignore", "-q", "--", d+"/probe"); err != nil {
			out = append(out, d)
		}
	}
	return out
}
