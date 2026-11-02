// Package gitutil wraps the few git queries heavyq needs.
package gitutil

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/LysCongThanh/heavyq/internal/config"
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
	// Submodules are never ignored, whatever submodule.<name>.ignore or
	// diff.ignoreSubmodules say: a changed submodule is a changed tree.
	args := append([]string{"status", "--porcelain", "-z", "--untracked-files=normal", "--ignore-submodules=none"},
		untrackedSpecs(exclude)...)
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
		if only, err := onlyNestedRepos(repo, path, exclude); err != nil || !only {
			return false, err
		}
	}
	return true, nil
}

// Sibling is a --with repository pinned to a commit.
type Sibling struct {
	Name string // directory name, recreated next to the main worktree remotely
	Path string // absolute local path
	SHA  string
}

// ResolveSibling parses "name[@ref]" (name is a directory next to mainRepo,
// or a path containing "/") into a pinned Sibling.
func ResolveSibling(spec, mainRepo string) (Sibling, error) {
	name, ref, hasAt := strings.Cut(spec, "@")
	if !hasAt {
		ref = "HEAD"
	}
	if name == "" || ref == "" {
		return Sibling{}, fmt.Errorf("--with %q: expected REPO[@REF]", spec)
	}
	p := name
	if !strings.Contains(name, "/") {
		if mainRepo == "" {
			return Sibling{}, fmt.Errorf("--with %q needs the main repo (run inside it or pass --repo)", spec)
		}
		p = filepath.Join(filepath.Dir(mainRepo), name)
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return Sibling{}, err
	}
	top, err := TopLevel(abs)
	if err != nil {
		return Sibling{}, fmt.Errorf("--with %q: %s is not a git repo", spec, abs)
	}
	base := filepath.Base(top)
	if !config.SafeName.MatchString(base) {
		return Sibling{}, fmt.Errorf("--with: directory name %q has unsafe characters", base)
	}
	if mainRepo != "" && base == filepath.Base(mainRepo) {
		return Sibling{}, fmt.Errorf("--with: %q has the same name as the main repo", base)
	}
	sha, err := ResolveCommit(top, ref)
	if err != nil {
		return Sibling{}, fmt.Errorf("--with %q: %w", spec, err)
	}
	return Sibling{Name: base, Path: top, SHA: sha}, nil
}

// LocalMismatch explains why the local work tree of s does not match its pin
// ("" when HEAD equals the pinned SHA and the tree is clean).
func (s Sibling) LocalMismatch() string {
	head, err := ResolveCommit(s.Path, "HEAD")
	if err != nil || head != s.SHA {
		return fmt.Sprintf("%s: HEAD differs from pinned %.12s", s.Name, s.SHA)
	}
	if !IsClean(s.Path) {
		return s.Name + ": uncommitted changes"
	}
	return ""
}
