package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/LysCongThanh/heavyq/internal/gitutil"
)

// runDir is the directory a run's command runs in: wd, unless repo (--repo) is
// set and wd is not inside it, then repo. Inside is decided by the directories
// themselves (os.SameFile up wd's parents), not by comparing path strings: on
// a case-insensitive file system (macOS) the shell's $PWD can spell a directory
// differently from the disk, and git reports the disk's spelling.
func runDir(wd, repo string) string {
	if repo == "" {
		return wd
	}
	abs, err := filepath.Abs(repo)
	if err != nil {
		return wd
	}
	if within(wd, abs) {
		return wd
	}
	return abs
}

// within reports whether dir is root or below it.
func within(dir, root string) bool {
	rootInfo, err := os.Stat(root)
	if err != nil {
		return false
	}
	for d := dir; ; d = filepath.Dir(d) {
		if fi, err := os.Stat(d); err == nil && os.SameFile(fi, rootInfo) {
			return true
		}
		if filepath.Dir(d) == d {
			return false
		}
	}
}

// remoteSubdir is the subdirectory of the repository a remote run of commit
// sha runs in: the one the run's directory dir is in, as git names it (the same
// prefix the ledger records), so the remote runs where a local run would. It
// must be a name the remote job accepts and a directory of that commit (an
// ignored or untracked-only directory, like build/, is not in it).
func remoteSubdir(dir, repo, sha string) (string, error) {
	prefix, err := gitutil.Prefix(dir)
	if err != nil {
		return "", fmt.Errorf("cannot tell which subdirectory of %s this is: %v", filepath.Base(repo), err)
	}
	switch {
	case prefix == "":
		return "", nil
	case !subdirRe.MatchString(prefix) || strings.Contains(prefix, ".."):
		return "", fmt.Errorf("current subdirectory %q has characters a remote run cannot use", prefix)
	}
	if !gitutil.HasDir(repo, sha, prefix) {
		return "", fmt.Errorf("current subdirectory %q is not in commit %.12s (ignored or untracked?), so a remote run cannot use it", prefix, sha)
	}
	return prefix, nil
}
