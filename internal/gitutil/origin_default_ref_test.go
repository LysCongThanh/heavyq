package gitutil

import (
	"path/filepath"
	"testing"
)

// originRepo is a real repository with a bare "origin" that has the branches in pushed (each a copy of main),
// every one of them fetched into refs/remotes/origin, and no refs/remotes/origin/HEAD yet.
func originRepo(t *testing.T, pushed ...string) string {
	t.Helper()
	dir := t.TempDir()
	repo, bare := filepath.Join(dir, "app"), filepath.Join(dir, "origin.git")
	gitIn(t, dir, "init", "-q", "--bare", bare)
	gitIn(t, dir, "init", "-q", "-b", "main", repo)
	write(t, filepath.Join(repo, "f"), "x")
	gitIn(t, repo, "add", ".")
	gitIn(t, repo, "commit", "-q", "-m", "init")
	gitIn(t, repo, "remote", "add", "origin", bare)
	for _, b := range pushed {
		gitIn(t, repo, "push", "-q", "origin", "main:"+b)
	}
	return repo
}

func TestOriginDefaultRef(t *testing.T) {
	setHead := func(branch string) func(t *testing.T, repo string) {
		return func(t *testing.T, repo string) { gitIn(t, repo, "remote", "set-head", "origin", branch) }
	}
	dangling := func(t *testing.T, repo string) { // origin/HEAD names trunk, which then goes away
		setHead("trunk")(t, repo)
		gitIn(t, repo, "update-ref", "-d", "refs/remotes/origin/trunk")
	}
	for _, c := range []struct {
		name   string
		pushed []string
		setup  func(t *testing.T, repo string)
		want   string // "" = no default
	}{
		{"origin/HEAD names main", []string{"main"}, setHead("main"), "origin/main"},
		{"origin/HEAD names another branch even when origin/main exists", []string{"main", "trunk"}, setHead("trunk"), "origin/trunk"},
		{"no origin/HEAD, origin/main exists", []string{"main"}, nil, "origin/main"},
		{"no origin/HEAD and no origin/main", []string{"trunk"}, nil, ""},
		{"a dangling origin/HEAD falls back to origin/main", []string{"main", "trunk"}, dangling, "origin/main"},
		{"a dangling origin/HEAD and no origin/main", []string{"trunk"}, dangling, ""},
		{"a local main is not origin's", nil, nil, ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			repo := originRepo(t, c.pushed...)
			if c.setup != nil {
				c.setup(t, repo)
			}
			ref, ok := OriginDefaultRef(repo)
			if ref != c.want || ok != (c.want != "") {
				t.Fatalf("OriginDefaultRef = %q, %v; want %q", ref, ok, c.want)
			}
		})
	}
	if ref, ok := OriginDefaultRef(t.TempDir()); ok {
		t.Fatalf("a directory that is no repository has no default, got %q", ref)
	}
}
