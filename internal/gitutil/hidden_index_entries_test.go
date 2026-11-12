package gitutil

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// assertUnknownTree fails unless every identity of dir refuses to name a tree:
// WorkTree is unknown, CleanTree is ErrDirty (so the cache never keys on it)
// and Identify records no tree.
func assertUnknownTree(t *testing.T, dir, what string) {
	t.Helper()
	if id := workTreeID(t, dir); id != (TreeID{Dirty: true}) {
		t.Fatalf("%s: WorkTree = %+v, want unknown", what, id)
	}
	if tree, err := CleanTree(dir); !errors.Is(err, ErrDirty) {
		t.Fatalf("%s: CleanTree = %q, %v; want ErrDirty", what, tree, err)
	}
	if co, err := Identify(dir); err != nil || co.Tree != "" || !co.Dirty || co.Commit == "" {
		t.Fatalf("%s: Identify = %+v, %v; want commit kept, no tree, dirty", what, co, err)
	}
}

// Audit: git status and git diff skip index entries flagged assume-unchanged
// or skip-worktree, and sparse checkout flags every path outside its cone. The
// work tree then holds edited or missing files while both commands report
// HEAD's tree, so a proof or a cache entry was recorded for code that was
// never there. Such a tree is unknown now.
func TestIndexFlagsThatHideFilesMakeTheTreeUnknown(t *testing.T) {
	for _, flag := range []string{"--assume-unchanged", "--skip-worktree"} {
		t.Run(flag, func(t *testing.T) {
			dir := repoWithFiles(t)
			clean := workTreeID(t, dir)
			gitIn(t, dir, "update-index", flag, "sub/b.txt")
			// Flagged, even before any edit: git would not tell us about one.
			assertUnknownTree(t, dir, flag+" alone")
			write(t, filepath.Join(dir, "sub", "b.txt"), "edited behind git's back\n")
			assertUnknownTree(t, dir, flag+" and an edit")
			gitIn(t, dir, "update-index", "--no"+flag[1:], "sub/b.txt")
			if id := workTreeID(t, dir); id.ID == "" || !id.Dirty || id == clean {
				t.Fatalf("flag cleared: %+v, want a dirty id", id)
			}
			gitIn(t, dir, "checkout", "--", "sub/b.txt")
			if id := workTreeID(t, dir); id != clean {
				t.Fatalf("edit undone: %+v, want the clean id %+v", id, clean)
			}
		})
	}
}

// A sparse checkout leaves files of HEAD out of the work tree: a command run
// there never saw them, so its pass cannot prove HEAD's full tree.
func TestSparseCheckoutMakesTheTreeUnknown(t *testing.T) {
	dir := repoWithFiles(t)
	write(t, filepath.Join(dir, "lib", "c.txt"), "gamma\n")
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-q", "-m", "lib")
	clean := workTreeID(t, dir)
	gitIn(t, dir, "sparse-checkout", "set", "--cone", "lib")
	if _, err := os.Stat(filepath.Join(dir, "sub", "b.txt")); !os.IsNotExist(err) {
		t.Fatalf("precondition: sub/b.txt should be outside the cone (%v)", err)
	}
	assertUnknownTree(t, dir, "sparse checkout")
	gitIn(t, dir, "sparse-checkout", "disable")
	if id := workTreeID(t, dir); id != clean {
		t.Fatalf("sparse checkout disabled: %+v, want the clean id %+v", id, clean)
	}
}

// heavyq's own bookkeeping is never part of the identity, so a flag on it
// hides nothing that counts.
func TestIndexFlagOnBookkeepingIsHarmless(t *testing.T) {
	dir := repoWithBookkeeping(t)
	clean := workTreeID(t, dir)
	gitIn(t, dir, "update-index", "--skip-worktree", ".heavyq/anchor.md")
	write(t, filepath.Join(dir, ".heavyq", "anchor.md"), anchorDone)
	if id := workTreeID(t, dir); id != clean {
		t.Fatalf("flagged bookkeeping file: %+v, want the clean id %+v", id, clean)
	}
	if tree, err := CleanTree(dir); err != nil || tree != clean.ID {
		t.Fatalf("CleanTree = %q, %v; want %q", tree, err, clean.ID)
	}
}

// Audit: a submodule that is not checked out (every `git worktree add`, so
// every agent work tree) gave the same id as the checked-out one, and both
// work trees share the repo id, so a pass in the agent work tree proved the
// main checkout, whose submodule files the command never saw.
func TestUncheckedOutSubmoduleIsNotTheCheckedOutTree(t *testing.T) {
	super, _ := superWithSubmodule(t)
	main, err := Identify(super)
	if err != nil || main.Tree == "" || main.Dirty {
		t.Fatalf("main checkout: %+v, %v", main, err)
	}
	wts := []string{filepath.Join(t.TempDir(), "wt1"), filepath.Join(t.TempDir(), "wt2")}
	var ids []TreeID
	for i, wt := range wts {
		gitIn(t, super, "worktree", "add", "-q", "-b", "agent"+string(rune('1'+i)), wt)
		if _, err := os.Lstat(filepath.Join(wt, "mod", ".git")); err == nil {
			t.Fatal("precondition: git worktree add should not check out submodules")
		}
		co, err := Identify(wt)
		if err != nil || co.Repo != main.Repo {
			t.Fatalf("linked work tree: %+v, %v (want the main checkout's repo %s)", co, err, main.Repo)
		}
		if co.Tree == "" || co.Tree == main.Tree || !co.Dirty {
			t.Fatalf("submodule not checked out: tree %q dirty %v, want a dirty id other than %q", co.Tree, co.Dirty, main.Tree)
		}
		if tree, err := CleanTree(wt); !errors.Is(err, ErrDirty) {
			t.Fatalf("CleanTree = %q, %v; want ErrDirty (the cache must not replay the main checkout's pass)", tree, err)
		}
		ids = append(ids, workTreeID(t, wt))
	}
	if ids[0] != ids[1] {
		t.Fatalf("two work trees with the same files: %+v vs %+v", ids[0], ids[1])
	}
	// Checking the submodule out gives the main checkout's files, and its id.
	gitIn(t, wts[0], "-c", "protocol.file.allow=always", "submodule", "update", "--init", "-q")
	if id := workTreeID(t, wts[0]); id.ID != main.Tree || id.Dirty {
		t.Fatalf("submodule checked out: %+v, want the clean id %s", id, main.Tree)
	}
	// An edit on top of a missing submodule is not the same edit with it present.
	write(t, filepath.Join(wts[0], "a.txt"), "edit\n")
	write(t, filepath.Join(wts[1], "a.txt"), "edit\n")
	if a, b := workTreeID(t, wts[0]), workTreeID(t, wts[1]); a.ID == "" || b.ID == "" || a == b {
		t.Fatalf("same edit, submodule present vs missing: %+v vs %+v, want two ids", a, b)
	}
}
