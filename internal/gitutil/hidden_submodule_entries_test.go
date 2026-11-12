package gitutil

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// Review: the superproject only asks a submodule `git status`, which trusts
// the submodule's own index the same way: its assume-unchanged and
// skip-worktree entries, and every path outside its sparse cone, read as
// unchanged. A submodule with edited or missing files behind such a flag gave
// the clean id, and CleanTree let the cache key on it.
func TestIndexFlagsInsideASubmoduleMakeTheTreeUnknown(t *testing.T) {
	for _, flag := range []string{"--assume-unchanged", "--skip-worktree"} {
		t.Run(flag, func(t *testing.T) {
			super, sub := superWithSubmodule(t)
			clean := workTreeID(t, super)
			gitIn(t, sub, "update-index", flag, "f.txt")
			assertUnknownTree(t, super, flag+" in the submodule")
			write(t, filepath.Join(sub, "f.txt"), "edited behind git's back\n")
			assertUnknownTree(t, super, flag+" and an edit in the submodule")
			gitIn(t, sub, "update-index", "--no"+flag[1:], "f.txt")
			gitIn(t, sub, "checkout", "-q", "--", "f.txt")
			if id := workTreeID(t, super); id != clean {
				t.Fatalf("flag cleared, edit undone: %+v, want the clean id %+v", id, clean)
			}
		})
	}
}

func TestSparseCheckoutInsideASubmoduleMakesTheTreeUnknown(t *testing.T) {
	super, sub := superWithSubmodule(t)
	clean := workTreeID(t, super)
	gitIn(t, sub, "sparse-checkout", "set", "--no-cone", "/nothing")
	if _, err := os.Stat(filepath.Join(sub, "f.txt")); !os.IsNotExist(err) {
		t.Fatalf("precondition: mod/f.txt should be outside the sparse checkout (%v)", err)
	}
	assertUnknownTree(t, super, "sparse checkout in the submodule")
	gitIn(t, sub, "sparse-checkout", "disable")
	if id := workTreeID(t, super); id != clean {
		t.Fatalf("sparse checkout disabled: %+v, want the clean id %+v", id, clean)
	}
}

// superWithNestedSubmodule makes a committed superproject whose submodule
// "mod" has a submodule of its own, "inner", left uninitialised: `git
// submodule add` and `git submodule update --init` without --recursive both
// leave it so.
func superWithNestedSubmodule(t *testing.T) (super, inner string) {
	t.Helper()
	leaf := t.TempDir()
	gitIn(t, leaf, "init", "-q", "-b", "main")
	write(t, filepath.Join(leaf, "g.txt"), "leaf\n")
	gitIn(t, leaf, "add", ".")
	gitIn(t, leaf, "commit", "-q", "-m", "leaf init")
	mid := t.TempDir()
	gitIn(t, mid, "init", "-q", "-b", "main")
	write(t, filepath.Join(mid, "f.txt"), "mid\n")
	gitIn(t, mid, "add", ".")
	gitIn(t, mid, "-c", "protocol.file.allow=always", "submodule", "add", "-q", leaf, "inner")
	gitIn(t, mid, "commit", "-q", "-m", "mid with inner")
	super = repoWithFiles(t)
	gitIn(t, super, "-c", "protocol.file.allow=always", "submodule", "add", "-q", mid, "mod")
	gitIn(t, super, "commit", "-q", "-m", "add submodule")
	return super, filepath.Join(super, "mod", "inner")
}

// Review: a submodule that is not checked out inside a checked-out one is
// missing its files as surely as one at the top, and `git status` in the outer
// submodule reports nothing for it either. It is dirty with an id of its own,
// never the clean id, until it is checked out too.
func TestNestedSubmoduleNotCheckedOutIsNotTheCheckedOutTree(t *testing.T) {
	super, inner := superWithNestedSubmodule(t)
	if _, err := os.Lstat(filepath.Join(inner, ".git")); err == nil {
		t.Fatal("precondition: the nested submodule should not be checked out")
	}
	missing := workTreeID(t, super)
	if missing.ID == "" || !missing.Dirty {
		t.Fatalf("nested submodule missing: %+v, want a dirty id", missing)
	}
	if tree, err := CleanTree(super); !errors.Is(err, ErrDirty) {
		t.Fatalf("CleanTree = %q, %v; want ErrDirty", tree, err)
	}
	gitIn(t, super, "-c", "protocol.file.allow=always", "submodule", "update", "--init", "--recursive", "-q")
	full := workTreeID(t, super)
	if full.Dirty || full.ID == "" || full.ID == missing.ID {
		t.Fatalf("nested submodule checked out: %+v, want a clean id other than %+v", full, missing)
	}
	if tree, err := CleanTree(super); err != nil || tree != full.ID {
		t.Fatalf("CleanTree = %q, %v; want %q", tree, err, full.ID)
	}
	// A flag two levels down hides files as well.
	gitIn(t, inner, "update-index", "--skip-worktree", "g.txt")
	assertUnknownTree(t, super, "skip-worktree in the nested submodule")
}
