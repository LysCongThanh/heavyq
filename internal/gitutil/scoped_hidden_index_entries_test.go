package gitutil

import (
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
