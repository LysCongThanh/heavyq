package gitutil

import (
	"io"
	"strings"
)

// DefaultBranchRef names the branch work lands on, as a short ref that
// resolves in repo: origin's HEAD when it is known, else the first of
// origin/main, origin/master, main, master that exists. ok is false when none
// does (a repository with no remote and an unusual branch name).
func DefaultBranchRef(repo string) (ref string, ok bool) {
	if sym, err := git(repo, "symbolic-ref", "--quiet", "--short", "refs/remotes/origin/HEAD"); err == nil && sym != "" {
		if _, err := ResolveCommit(repo, sym); err == nil {
			return sym, true
		}
	}
	for _, c := range []string{"origin/main", "origin/master", "main", "master"} {
		if _, err := ResolveCommit(repo, c); err == nil {
			return c, true
		}
	}
	return "", false
}

// IsAncestor reports whether commit is reachable from ref: the work is on
// that branch. A squash merge leaves the original commits unreachable, so
// false means "not there as such", never "not merged".
func IsAncestor(repo, commit, ref string) bool {
	if strings.HasPrefix(commit, "-") || strings.HasPrefix(ref, "-") || commit == "" {
		return false
	}
	return gitTo(io.Discard, repo, "merge-base", "--is-ancestor", commit, ref) == nil
}
