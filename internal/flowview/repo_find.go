package flowview

import (
	"os"
	"path/filepath"
	"strings"
)

// maxWalkUp bounds the climb from a directory to its repository root.
const maxWalkUp = 64

// CommonDirOf finds the git common dir of the repository around dir by
// reading the file system only, never by running git (the status line has
// 5 ms). It matches `git rev-parse --git-common-dir`: a `.git` directory is
// the common dir; a `.git` file ("gitdir: <path>", a linked work tree or a
// submodule) points to a git dir whose `commondir` file, when present, names
// the shared one. ok is false outside a repository.
func CommonDirOf(dir string) (common string, ok bool) {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return "", false
	}
	for i := 0; i < maxWalkUp; i++ {
		if c, found := commonDirAt(filepath.Join(dir, ".git")); found {
			if real, err := filepath.EvalSymlinks(c); err == nil {
				c = real
			}
			return c, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", false
}

// commonDirAt resolves one `.git` entry.
func commonDirAt(dotGit string) (string, bool) {
	st, err := os.Stat(dotGit)
	if err != nil {
		return "", false
	}
	if st.IsDir() {
		return dotGit, true
	}
	b, err := os.ReadFile(dotGit)
	if err != nil || len(b) > 4096 {
		return "", false
	}
	line, _, _ := strings.Cut(strings.TrimSpace(string(b)), "\n")
	target, found := strings.CutPrefix(line, "gitdir:")
	if !found {
		return "", false
	}
	gitDir := strings.TrimSpace(target)
	if !filepath.IsAbs(gitDir) {
		gitDir = filepath.Join(filepath.Dir(dotGit), gitDir)
	}
	if cd, err := os.ReadFile(filepath.Join(gitDir, "commondir")); err == nil {
		p := strings.TrimSpace(string(cd))
		if !filepath.IsAbs(p) {
			p = filepath.Join(gitDir, p)
		}
		return filepath.Clean(p), true
	}
	return filepath.Clean(gitDir), true
}
