package gitutil

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/LysCongThanh/heavyq/internal/config"
)

// Sibling is a --with repository pinned to a commit.
type Sibling struct {
	Name string // directory name, recreated next to the main worktree remotely
	Path string // absolute local path of the repository root
	SHA  string
	// NextToMain: Path is <parent of the main repo>/<Name>, so a local command
	// that reads ../<Name> really sees this repository.
	NextToMain bool
}

// Label is "name@sha12", the form shown in status and history.
func (s Sibling) Label() string { return s.Name + "@" + s.SHA[:12] }

// ResolveSibling parses spec and pins it: the repository root must exist, be
// a git repository root, differ from the main repository and its name be safe
// to recreate on a remote; the ref is resolved to a full commit now.
// mainRepo may be "" only for a path spec (a bare name is next to it).
func ResolveSibling(spec, mainRepo string) (Sibling, error) {
	repo, ref, err := config.ParseSiblingSpec(spec)
	if err != nil {
		return Sibling{}, err
	}
	p := repo
	if !strings.Contains(repo, "/") {
		if mainRepo == "" {
			return Sibling{}, fmt.Errorf("--with %q needs the main repo (run inside it or pass --repo)", spec)
		}
		p = filepath.Join(filepath.Dir(mainRepo), repo)
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return Sibling{}, err
	}
	if fi, err := os.Stat(abs); err != nil || !fi.IsDir() {
		return Sibling{}, fmt.Errorf("--with %q: %s is not an existing directory", spec, abs)
	}
	top, err := TopLevel(abs)
	if err != nil {
		return Sibling{}, fmt.Errorf("--with %q: %s is not a git repository", spec, abs)
	}
	if !sameDir(top, abs) {
		return Sibling{}, fmt.Errorf("--with %q: %s is inside the repository %s, not its root", spec, abs, top)
	}
	name := filepath.Base(top)
	if !config.SafeName.MatchString(name) {
		return Sibling{}, fmt.Errorf("--with %q: directory name %q has unsafe characters", spec, name)
	}
	if mainRepo != "" && (name == filepath.Base(mainRepo) || sameDir(top, mainRepo)) {
		return Sibling{}, fmt.Errorf("--with %q: that is the main repository (same name or path)", spec)
	}
	sha, err := ResolveCommit(top, ref)
	if err != nil {
		return Sibling{}, fmt.Errorf("--with %q: %w", spec, err)
	}
	s := Sibling{Name: name, Path: top, SHA: sha}
	s.NextToMain = mainRepo != "" && sameDir(top, filepath.Join(filepath.Dir(mainRepo), name))
	return s, nil
}

// sameDir reports whether a and b are the same directory, symlinks resolved.
func sameDir(a, b string) bool {
	ra, errA := filepath.EvalSymlinks(a)
	rb, errB := filepath.EvalSymlinks(b)
	return errA == nil && errB == nil && ra == rb
}

// LocalMismatch explains why running a local command with s is not the same
// as running it on s's pinned commit ("" when it is): the repository must be
// where ../<name> points, HEAD must equal the pin and the tree must be clean.
func (s Sibling) LocalMismatch() string {
	if !s.NextToMain {
		return fmt.Sprintf("%s: lives at %s, not next to the main repository, so ../%s is another directory", s.Name, s.Path, s.Name)
	}
	head, err := ResolveCommit(s.Path, "HEAD")
	if err != nil || head != s.SHA {
		return fmt.Sprintf("%s: HEAD differs from pinned %.12s", s.Name, s.SHA)
	}
	if !IsClean(s.Path) {
		return s.Name + ": uncommitted changes"
	}
	return ""
}
