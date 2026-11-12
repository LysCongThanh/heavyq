// Package gitutil wraps the few git queries heavyq needs.
package gitutil

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"
)

var shaRe = regexp.MustCompile(`^[0-9a-f]{40}([0-9a-f]{24})?$`)

// gitTo runs git in dir and streams its stdout to w. heavyq only reads, so
// GIT_OPTIONAL_LOCKS=0 keeps git from taking the index lock to refresh stat
// data: a concurrent `git add` or `git commit` (the user's, or another agent's
// in the same work tree) never fails with "index.lock exists" because of us.
func gitTo(w io.Writer, dir string, args ...string) error {
	return gitWithInput(w, nil, dir, args...)
}

// gitWithInput is gitTo with git's standard input read from in (nil: none).
func gitWithInput(w io.Writer, in io.Reader, dir string, args ...string) error {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0")
	var errb bytes.Buffer
	cmd.Stdin, cmd.Stdout, cmd.Stderr = in, w, &errb
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(errb.String())
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("git %s: %s", strings.Join(args, " "), msg)
	}
	return nil
}

// gitRaw returns git's stdout untouched, for NUL- or line-structured output.
func gitRaw(dir string, args ...string) (string, error) {
	var out bytes.Buffer
	if err := gitTo(&out, dir, args...); err != nil {
		return "", err
	}
	return out.String(), nil
}

func git(dir string, args ...string) (string, error) {
	out, err := gitRaw(dir, args...)
	return strings.TrimSpace(out), err
}

// TopLevel returns the absolute work-tree root containing dir.
func TopLevel(dir string) (string, error) { return git(dir, "rev-parse", "--show-toplevel") }

// Prefix returns dir's path relative to its repo root ("" at the root).
func Prefix(dir string) (string, error) {
	p, err := git(dir, "rev-parse", "--show-prefix")
	return strings.TrimSuffix(p, "/"), err
}

// ResolveCommit resolves ref to a full commit SHA.
func ResolveCommit(repo, ref string) (string, error) {
	if strings.HasPrefix(ref, "-") {
		return "", fmt.Errorf("invalid ref %q", ref)
	}
	sha, err := git(repo, "rev-parse", "--verify", "--quiet", ref+"^{commit}")
	if err != nil || !shaRe.MatchString(sha) {
		return "", fmt.Errorf("%s: no commit %q", repo, ref)
	}
	return sha, nil
}

// TreeOf resolves ref to the SHA of its root tree.
func TreeOf(repo, ref string) (string, error) {
	if strings.HasPrefix(ref, "-") {
		return "", fmt.Errorf("invalid ref %q", ref)
	}
	sha, err := git(repo, "rev-parse", "--verify", "--quiet", ref+"^{tree}")
	if err != nil || !shaRe.MatchString(sha) {
		return "", fmt.Errorf("%s: no tree for %q", repo, ref)
	}
	return sha, nil
}

// IsClean reports whether the work tree has no modified or untracked files.
func IsClean(repo string) bool {
	clean, err := workTreeClean(repo)
	return err == nil && clean
}

func workTreeClean(repo string) (bool, error) { return workTreeCleanExcept(repo, nil) }

// workTreeCleanExcept is workTreeClean that does not count changes to the
// paths in exclude (the bookkeeping paths, see bookkeepingPaths), nor nested
// work trees and repositories (see NestedWorkTreeDirs and isNestedRepo).
func workTreeCleanExcept(repo string, exclude []string) (bool, error) {
	return workTreeCleanSpecs(repo, untrackedSpecs(exclude))
}

// workTreeCleanSpecs is workTreeCleanExcept for the paths specs (pathspecs after "--") let through.
func workTreeCleanSpecs(repo string, specs []string) (bool, error) {
	// Submodules are never ignored, whatever submodule.<name>.ignore or
	// diff.ignoreSubmodules say: a changed submodule is a changed tree.
	args := append([]string{"status", "--porcelain", "-z", "--untracked-files=normal", "--ignore-submodules=none"}, specs...)
	out, err := gitRaw(repo, args...)
	if err != nil {
		return false, err
	}
	for _, entry := range strings.Split(out, "\x00") {
		if entry == "" {
			continue
		}
		path, untracked := strings.CutPrefix(entry, "?? ")
		if !untracked || !strings.HasSuffix(path, "/") {
			return false, nil // a change to a tracked file, or an untracked file
		}
		// An untracked directory: clean only when all it holds is nested repositories.
		if only, err := onlyNestedRepos(repo, path, specs); err != nil || !only {
			return false, err
		}
	}
	return true, nil
}
