package gitutil

import (
	"os"
	"path/filepath"
	"testing"
)

func countUncommitted(t *testing.T, dir string) int {
	t.Helper()
	n, err := UncommittedFiles(dir)
	if err != nil {
		t.Fatal(err)
	}
	if (n == 0) != IsClean(dir) {
		t.Fatalf("UncommittedFiles = %d but IsClean = %v", n, IsClean(dir))
	}
	return n
}

// Every kind of change counts once, and what git ignores counts never.
func TestUncommittedFilesCountsEachKindOfChangeOnce(t *testing.T) {
	dir := repoWithFiles(t)
	if n := countUncommitted(t, dir); n != 0 {
		t.Fatalf("clean tree: %d", n)
	}
	write(t, filepath.Join(dir, "ignored.log"), "noise\n")
	write(t, filepath.Join(dir, "build", "out.bin"), "noise\n")
	if n := countUncommitted(t, dir); n != 0 {
		t.Fatalf("ignored files only: %d", n)
	}
	write(t, filepath.Join(dir, "a.txt"), "edited\n") // modified, unstaged
	if n := countUncommitted(t, dir); n != 1 {
		t.Fatalf("one modified file: %d", n)
	}
	gitIn(t, dir, "add", "a.txt") // staged: still the same one file
	write(t, filepath.Join(dir, "a.txt"), "edited again\n")
	if n := countUncommitted(t, dir); n != 1 {
		t.Fatalf("staged and modified again: %d", n)
	}
	write(t, filepath.Join(dir, "scratch", "one.txt"), "1\n") // an untracked directory counts by file
	write(t, filepath.Join(dir, "scratch", "two.txt"), "2\n")
	if n := countUncommitted(t, dir); n != 3 {
		t.Fatalf("modified + two untracked files in a directory: %d", n)
	}
	if err := os.Remove(filepath.Join(dir, "sub", "b.txt")); err != nil { // deleted
		t.Fatal(err)
	}
	if n := countUncommitted(t, dir); n != 4 {
		t.Fatalf("plus a deleted file: %d", n)
	}
}

// A staged rename is one file, not the old path and the new one.
func TestUncommittedFilesCountsARenameOnce(t *testing.T) {
	dir := repoWithFiles(t)
	gitIn(t, dir, "mv", "a.txt", "renamed.txt")
	if n := countUncommitted(t, dir); n != 1 {
		t.Fatalf("a staged rename: %d", n)
	}
	write(t, filepath.Join(dir, "new.txt"), "n\n") // the entry after the rename must still be read
	if n := countUncommitted(t, dir); n != 2 {
		t.Fatalf("a rename and an untracked file: %d", n)
	}
}

// Another repository inside the work tree is not part of this one, and neither
// is where agents keep their own work trees.
func TestUncommittedFilesLeavesNestedRepositoriesAndAgentWorktreesOut(t *testing.T) {
	dir := repoWithFiles(t)
	gitIn(t, dir, "init", "-q", "-b", "main", filepath.Join(dir, "vendor", "lib"))
	write(t, filepath.Join(dir, "vendor", "lib", "x.txt"), "x\n")
	write(t, filepath.Join(dir, ".claude", "worktrees", "wt", "y.txt"), "y\n")
	if n := countUncommitted(t, dir); n != 0 {
		t.Fatalf("nested repository and agent work tree: %d", n)
	}
	write(t, filepath.Join(dir, "vendor", "own.txt"), "mine\n")
	if n := countUncommitted(t, dir); n != 1 {
		t.Fatalf("an untracked file beside the nested repository: %d", n)
	}
}

func TestUncommittedFilesOutsideARepositoryIsAnError(t *testing.T) {
	if n, err := UncommittedFiles(t.TempDir()); err == nil {
		t.Fatalf("a plain directory counted %d files", n)
	}
}
