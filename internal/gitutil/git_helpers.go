// Package gitutil wraps the few git queries heavyq needs.
package gitutil

import (
	"bytes"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/LysCongThanh/heavyq/internal/config"
)

var shaRe = regexp.MustCompile(`^[0-9a-f]{40}([0-9a-f]{24})?$`)

func git(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(errb.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), msg)
	}
	return strings.TrimSpace(out.String()), nil
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
	out, err := git(repo, "status", "--porcelain", "--untracked-files=normal")
	return err == nil && out == ""
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
