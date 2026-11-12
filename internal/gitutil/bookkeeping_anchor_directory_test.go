package gitutil

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

// An [anchor] file that names a directory (a typo, or a plan directory that
// also holds scripts) must not take that directory out of the tree identity:
// everything below it is code, committed, edited or new.
func TestAnAnchorThatIsADirectoryKeepsItsFilesInTheIdentity(t *testing.T) {
	for _, file := range []string{"src", "src/", "notes.md"} {
		t.Run(file, func(t *testing.T) {
			dir := repoWithFiles(t)
			sub := strings.TrimSuffix(file, "/")
			code := filepath.Join(dir, sub, "main.go")
			write(t, filepath.Join(dir, "heavyq.toml"), "[anchor]\nfile = \""+file+"\"\n")
			write(t, code, "package main\n")
			gitIn(t, dir, "add", ".")
			gitIn(t, dir, "commit", "-q", "-m", "anchor names a directory")
			base := mustID(t, dir)
			first, _ := ResolveCommit(dir, "HEAD")

			for name, edit := range map[string]func(){
				"edited file": func() { write(t, code, "package main // edited\n") },
				"new file":    func() { write(t, filepath.Join(dir, sub, "new.go"), "package main\n") },
			} {
				edit()
				if got := mustID(t, dir); got.ID == "" || got.ID == base.ID || !got.Dirty {
					t.Fatalf("%s under %s left the identity unchanged: %+v (base %+v)", name, sub, got, base)
				}
				if tree, err := CleanTree(dir); !errors.Is(err, ErrDirty) {
					t.Fatalf("%s under %s: CleanTree = %q, %v; want ErrDirty", name, sub, tree, err)
				}
				gitIn(t, dir, "checkout", "-q", "--", ".")
				gitIn(t, dir, "clean", "-qfd", "--", sub)
			}

			write(t, code, "package main // committed\n")
			gitIn(t, dir, "commit", "-qam", "change under the directory")
			if got := mustID(t, dir); got.ID == base.ID || got.Dirty {
				t.Fatalf("a committed change under %s kept the identity: %+v", sub, got)
			}
			second, _ := ResolveCommit(dir, "HEAD")
			a, errA := Pinned(dir, first)
			b, errB := Pinned(dir, second)
			if errA != nil || errB != nil {
				t.Fatal(errA, errB)
			}
			if a.Tree == b.Tree {
				t.Fatalf("pinned commits that differ under %s share the tree %s", sub, a.Tree)
			}
			if a.Tree != base.ID {
				t.Fatalf("pinned %s, local clean id of the same commit %s", a.Tree, base.ID)
			}
		})
	}
}
