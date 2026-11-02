package gitutil

import (
	"path/filepath"
	"strings"
)

// CommonDir returns the absolute git common dir of the repository around dir:
// the one directory every work tree of a repository shares (.git of the main
// checkout). State that must be seen by all work trees, and never committed,
// lives under it.
func CommonDir(dir string) (string, error) {
	out, err := git(dir, "rev-parse", "--git-common-dir")
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(out) {
		out = filepath.Join(dir, out) // relative to git's working directory
	}
	if abs, err := filepath.Abs(out); err == nil {
		out = abs
	}
	if real, err := filepath.EvalSymlinks(out); err == nil {
		out = real
	}
	return out, nil
}

// Worktrees lists the absolute root of every work tree of the repository
// around dir (the main one first), as `git worktree list` reports them.
// Bare repositories and prunable (deleted) work trees are left out.
func Worktrees(dir string) ([]string, error) {
	out, err := gitRaw(dir, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}
	var paths []string
	for _, block := range strings.Split(out, "\n\n") {
		var path string
		skip := false
		for _, ln := range strings.Split(block, "\n") {
			switch {
			case strings.HasPrefix(ln, "worktree "):
				path = strings.TrimPrefix(ln, "worktree ")
			case ln == "bare", strings.HasPrefix(ln, "prunable"):
				skip = true
			}
		}
		if path != "" && !skip {
			paths = append(paths, path)
		}
	}
	return paths, nil
}
