package gitutil

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultBranchRefAndAncestry(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "r")
	onMain := mkRepo(t, repo)
	gitIn(t, repo, "checkout", "-q", "-b", "feature")
	if err := os.WriteFile(filepath.Join(repo, "g"), []byte("y"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, repo, "add", ".")
	gitIn(t, repo, "commit", "-q", "-m", "feature work")
	onFeature := gitIn(t, repo, "rev-parse", "HEAD")

	ref, ok := DefaultBranchRef(repo)
	if !ok || ref != "main" {
		t.Fatalf("default branch ref = %q, %v; want main", ref, ok)
	}
	if !IsAncestor(repo, onMain, ref) || !IsAncestor(repo, onMain[:7], ref) {
		t.Error("a commit of main is on main, full or short")
	}
	if IsAncestor(repo, onFeature, ref) {
		t.Error("a feature commit is not on main yet")
	}
	for _, bad := range []string{"", "--output=/tmp/x", "nonsense"} {
		if IsAncestor(repo, bad, ref) {
			t.Errorf("%q must not count as merged", bad)
		}
	}
	gitIn(t, repo, "checkout", "-q", "main")
	gitIn(t, repo, "merge", "-q", "--ff-only", "feature")
	if !IsAncestor(repo, onFeature, ref) {
		t.Error("after the merge the feature commit is on main")
	}

	// No default branch under any usual name: not known, not guessed.
	gitIn(t, repo, "branch", "-q", "-m", "main", "trunk")
	if ref, ok := DefaultBranchRef(repo); ok {
		t.Errorf("a repository whose branch is called trunk has no known default, got %q", ref)
	}
}
