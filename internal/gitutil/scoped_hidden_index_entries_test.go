package gitutil

import (
	"os"
	"path/filepath"
	"testing"
)

// A gate scoped by paths has its own clean check (git status on the scope), which trusts the index just like
// the whole tree's: a file of the scope flagged assume-unchanged or skip-worktree was edited behind git's back
// and the scoped gate still got the commit's clean id, so its proof outlived code that was never proven.
// Such a scoped tree is unknown; a flag on a file outside the scope hides nothing the scope counts.
func TestScopedIdentityRefusesHiddenFilesInTheScope(t *testing.T) {
	for _, flag := range []string{"--assume-unchanged", "--skip-worktree"} {
		t.Run(flag, func(t *testing.T) {
			dir := repoForScopes(t)
			clean := scoped(t, dir, goScope)

			gitIn(t, dir, "update-index", flag, "docs/guide.md")
			write(t, filepath.Join(dir, "docs", "guide.md"), "# guide, edited behind git's back\n")
			if co := scoped(t, dir, goScope); co != clean {
				t.Fatalf("flag outside the scope: %+v, want the clean scoped id %+v", co, clean)
			}

			gitIn(t, dir, "update-index", flag, "pkg/lib.go")
			if co := scoped(t, dir, goScope); co.Tree != "" || !co.Dirty || co.Commit != clean.Commit {
				t.Fatalf("flag in the scope, no edit: %+v, want commit kept, no tree, dirty", co)
			}
			write(t, filepath.Join(dir, "pkg", "lib.go"), "package pkg\n\nvar Hidden = 1\n")
			if co := scoped(t, dir, goScope); co.Tree != "" || !co.Dirty {
				t.Fatalf("flagged file of the scope edited: %+v, want no tree, dirty", co)
			}
			// A scoped dirty tree (an edit git does see) is unknown too while the flag stays.
			write(t, filepath.Join(dir, "main.go"), "package main\n\nfunc main() {}\n")
			if co := scoped(t, dir, goScope); co.Tree != "" || !co.Dirty {
				t.Fatalf("flag and a visible edit in the scope: %+v, want no tree, dirty", co)
			}

			gitIn(t, dir, "update-index", "--no"+flag[1:], "pkg/lib.go")
			gitIn(t, dir, "checkout", "--", "pkg/lib.go", "main.go")
			if co := scoped(t, dir, goScope); co != clean {
				t.Fatalf("flag cleared, edits undone: %+v, want the clean scoped id %+v", co, clean)
			}
		})
	}
}

// Reading the index through the scope must not narrow what a scoped tree already refused: a submodule with
// changes of its own makes it unknown whatever paths say (docs/gates.md), once a listed file is edited too.
func TestScopedIdentityStillRefusesAChangedSubmoduleOutsideTheScope(t *testing.T) {
	super, sub := superWithSubmodule(t)
	scope := []string{"a.txt"}
	clean := scoped(t, super, scope)
	write(t, filepath.Join(sub, "f.txt"), "edited in the submodule\n")
	write(t, filepath.Join(super, "a.txt"), "edited in the scope\n")
	if co := scoped(t, super, scope); co.Tree != "" || !co.Dirty || co.Commit != clean.Commit {
		t.Fatalf("changed submodule outside the scope: %+v, want commit kept, no tree, dirty", co)
	}
	gitIn(t, sub, "checkout", "-q", "--", "f.txt")
	if co := scoped(t, super, scope); co.Tree == "" || !co.Dirty || co.Tree == clean.Tree {
		t.Fatalf("submodule restored: %+v, want a dirty scoped id", co)
	}
}

// git status skips a submodule that is not checked out, so a linked work tree (git worktree add checks none out)
// looked clean to a scope that lists the submodule, and got the main checkout's clean scoped id: a proof of the
// submodule's code replayed where that code is missing. A scope listing it gets a dirty id of its own; a scope
// without it is untouched by the missing checkout.
func TestScopedIdentityOfASubmoduleThatIsNotCheckedOutIsDirty(t *testing.T) {
	super, _ := superWithSubmodule(t)
	withMod, withoutMod := []string{"mod", "a.txt"}, []string{"a.txt"}
	mainWith, mainWithout := scoped(t, super, withMod), scoped(t, super, withoutMod)
	if mainWith.Dirty || mainWithout.Dirty {
		t.Fatalf("precondition: main checkout scoped ids %+v, %+v should be clean", mainWith, mainWithout)
	}

	wt := filepath.Join(t.TempDir(), "wt")
	gitIn(t, super, "worktree", "add", "-q", "-b", "agent", wt)
	if _, err := os.Lstat(filepath.Join(wt, "mod", ".git")); err == nil {
		t.Fatal("precondition: git worktree add should not check out submodules")
	}
	if co := scoped(t, wt, withMod); co.Tree == "" || !co.Dirty || co.Tree == mainWith.Tree || co.Commit != mainWith.Commit {
		t.Fatalf("scope lists the missing submodule: %+v, want a dirty id other than %q on commit %s", co, mainWith.Tree, mainWith.Commit)
	}
	if co := scoped(t, wt, withoutMod); co.Tree != mainWithout.Tree || co.Dirty {
		t.Fatalf("scope without the submodule: %+v, want the clean scoped id %q", co, mainWithout.Tree)
	}
}
