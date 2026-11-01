package gitutil

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// superWithSubmodule makes a committed superproject whose "mod" is a real
// submodule holding f.txt, at mod.
func superWithSubmodule(t *testing.T) (super, sub string) {
	t.Helper()
	src := t.TempDir()
	gitIn(t, src, "init", "-q", "-b", "main")
	write(t, filepath.Join(src, "f.txt"), "one\n")
	gitIn(t, src, "add", ".")
	gitIn(t, src, "commit", "-q", "-m", "sub init")
	super = repoWithFiles(t)
	gitIn(t, super, "-c", "protocol.file.allow=always", "submodule", "add", "-q", src, "mod")
	gitIn(t, super, "commit", "-q", "-m", "add submodule")
	return super, filepath.Join(super, "mod")
}

// Review: staged then reverted by hand is the same files as HEAD.
func TestStagedThenRevertedIsTheCleanIdentity(t *testing.T) {
	dir := repoWithFiles(t)
	head := workTreeID(t, dir).ID
	a := filepath.Join(dir, "a.txt")
	write(t, a, "changed\n")
	gitIn(t, dir, "add", "a.txt")
	write(t, a, "alpha\n") // back to HEAD's content: status says MM, the files equal HEAD
	if _, err := CleanTree(dir); err == nil {
		t.Fatal("precondition: git status should still report the tree as not clean")
	}
	if id := workTreeID(t, dir); id.ID != head || id.Dirty {
		t.Fatalf("files equal HEAD but identity is %+v, want {%s false}", id, head)
	}
}

// Review: changes inside a submodule were invisible (the diff only says
// "-dirty"), so different states shared one id. They are unknown now.
func TestChangedSubmoduleIsUnknownNeverAnId(t *testing.T) {
	super, sub := superWithSubmodule(t)
	clean := workTreeID(t, super)
	if clean.Dirty || clean.ID == "" {
		t.Fatalf("clean superproject: %+v", clean)
	}
	unknown := TreeID{Dirty: true}

	write(t, filepath.Join(sub, "f.txt"), "two\n")
	if id := workTreeID(t, super); id != unknown {
		t.Fatalf("edited submodule file: %+v, want unknown", id)
	}
	write(t, filepath.Join(sub, "f.txt"), "three\n")
	if id := workTreeID(t, super); id != unknown {
		t.Fatalf("edited again: %+v, want unknown", id)
	}
	gitIn(t, sub, "checkout", "-q", "--", "f.txt")
	write(t, filepath.Join(sub, "new.txt"), "untracked in the submodule\n")
	if id := workTreeID(t, super); id != unknown {
		t.Fatalf("untracked file in submodule: %+v, want unknown", id)
	}
	if co, err := Identify(super); err != nil || co.Tree != "" || !co.Dirty || co.Commit == "" {
		t.Fatalf("Identify: %+v, %v (want commit kept, tree empty, dirty)", co, err)
	}
	if err := os.Remove(filepath.Join(sub, "new.txt")); err != nil {
		t.Fatal(err)
	}
	if id := workTreeID(t, super); id != clean {
		t.Fatalf("reverted submodule: %+v, want %+v", id, clean)
	}
}

// The user can tell git to ignore submodule changes; the identity must not.
func TestSubmoduleIgnoreSettingsDoNotHideChanges(t *testing.T) {
	super, sub := superWithSubmodule(t)
	for _, cfg := range [][]string{{"submodule.mod.ignore", "all"}, {"diff.ignoreSubmodules", "all"}, {"submodule.mod.ignore", "dirty"}} {
		gitIn(t, super, "config", cfg[0], cfg[1])
		write(t, filepath.Join(sub, "f.txt"), "edited under "+cfg[1]+"\n")
		if _, err := CleanTree(super); err == nil {
			t.Fatalf("%v: a changed submodule counted as a clean tree", cfg)
		}
		if id := workTreeID(t, super); id.ID != "" || !id.Dirty {
			t.Fatalf("%v: %+v, want unknown", cfg, id)
		}
		gitIn(t, sub, "checkout", "-q", "--", "f.txt")
		gitIn(t, super, "config", "--unset", cfg[0])
	}
}

// A submodule moved to another commit has no hidden state: the diff names the
// commit, so the tree gets an id, and each commit gets a different one.
func TestSubmoduleAtAnotherCommitIsIdentified(t *testing.T) {
	super, sub := superWithSubmodule(t)
	clean := workTreeID(t, super)
	var ids []string
	for _, body := range []string{"two\n", "three\n"} {
		write(t, filepath.Join(sub, "f.txt"), body)
		gitIn(t, sub, "commit", "-q", "-am", "move "+strings.TrimSpace(body))
		id := workTreeID(t, super)
		if !id.Dirty || id.ID == "" || id.ID == clean.ID {
			t.Fatalf("submodule at a new commit: %+v", id)
		}
		ids = append(ids, id.ID)
	}
	if ids[0] == ids[1] {
		t.Fatal("two submodule commits share an id")
	}
}

// A submodule that is not checked out cannot be edited.
func TestUninitialisedSubmoduleDoesNotBlockTheIdentity(t *testing.T) {
	super, sub := superWithSubmodule(t)
	if err := os.RemoveAll(sub); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(super, "a.txt"), "edit\n")
	if id := workTreeID(t, super); id.ID == "" || !id.Dirty {
		t.Fatalf("an empty submodule directory made the tree unknown: %+v", id)
	}
}

// Review: an untracked nested repository is one opaque directory entry; its
// edits never changed the id.
func TestNestedUntrackedRepositoryIsUnknown(t *testing.T) {
	dir := repoWithFiles(t)
	clean := workTreeID(t, dir)
	nested := filepath.Join(dir, "vendor", "lib")
	gitIn(t, dir, "init", "-q", "-b", "main", nested)
	write(t, filepath.Join(nested, "x.txt"), "one\n")
	for _, body := range []string{"one\n", "two\n"} {
		write(t, filepath.Join(nested, "x.txt"), body)
		if id := workTreeID(t, dir); id.ID != "" || !id.Dirty {
			t.Fatalf("nested repo (%q): %+v, want unknown", body, id)
		}
	}
	if err := os.RemoveAll(filepath.Join(dir, "vendor")); err != nil {
		t.Fatal(err)
	}
	if id := workTreeID(t, dir); id != clean {
		t.Fatalf("after removing it: %+v, want %+v", id, clean)
	}
}

// Review: HEAD was resolved separately by each git call. A commit that lands
// during identification must give "unknown", never an id built from two HEADs.
func TestHeadMovingDuringIdentificationIsUnknown(t *testing.T) {
	dir := repoWithFiles(t)
	old, err := ResolveCommit(dir, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(dir, "a.txt"), "second commit\n")
	gitIn(t, dir, "commit", "-q", "-am", "second")
	if id, err := workTreeAt(dir, old); err != nil || id.ID != "" || !id.Dirty {
		t.Fatalf("clean tree, HEAD moved: %+v, %v", id, err)
	}
	write(t, filepath.Join(dir, "a.txt"), "dirty edit\n")
	if id, err := workTreeAt(dir, old); err != nil || id.ID != "" || !id.Dirty {
		t.Fatalf("dirty tree, HEAD moved: %+v, %v", id, err)
	}
	cur, _ := ResolveCommit(dir, "HEAD")
	if id, err := workTreeAt(dir, cur); err != nil || id.ID == "" {
		t.Fatalf("with the current HEAD the id is known: %+v, %v", id, err)
	}
	// An empty commit keeps the tree but is still a different HEAD.
	gitIn(t, dir, "checkout", "-q", "--", ".")
	gitIn(t, dir, "commit", "-q", "--allow-empty", "-m", "empty")
	if id, err := workTreeAt(dir, cur); err != nil || id.ID != "" {
		t.Fatalf("empty commit during identification: %+v, %v", id, err)
	}
}

// Review: diff.algorithm, indentHeuristic, submodule, quotePath, context and
// prefixes changed the bytes hashed, so one tree had one id per git config.
func TestUserDiffConfigDoesNotChangeTheIdentity(t *testing.T) {
	dir := repoWithFiles(t)
	var lines []string
	for i := 0; i < 40; i++ {
		lines = append(lines, "line", "  indented", "")
	}
	write(t, filepath.Join(dir, "é.txt"), strings.Join(lines, "\n"))
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-q", "-m", "more files")
	edited := append([]string{}, lines...)
	edited[3], edited[40], edited[90] = "changed one", "changed two", "changed three"
	write(t, filepath.Join(dir, "é.txt"), strings.Join(edited, "\n"))
	write(t, filepath.Join(dir, "a.txt"), "alpha\nadded\n")

	base := workTreeID(t, dir)
	if !base.Dirty || base.ID == "" {
		t.Fatalf("base: %+v", base)
	}
	for _, cfg := range [][2]string{
		{"diff.algorithm", "patience"}, {"diff.algorithm", "histogram"}, {"diff.indentHeuristic", "false"},
		{"diff.submodule", "diff"}, {"core.quotePath", "false"}, {"diff.noprefix", "true"},
		{"diff.mnemonicPrefix", "true"}, {"diff.context", "0"}, {"diff.interHunkContext", "9"},
		{"diff.renames", "copies"}, {"diff.external", "false"}, {"diff.ignoreSubmodules", "all"},
	} {
		gitIn(t, dir, "config", cfg[0], cfg[1])
		if id := workTreeID(t, dir); id != base {
			t.Errorf("%s=%s changed the identity: %+v vs %+v", cfg[0], cfg[1], id, base)
		}
		gitIn(t, dir, "config", "--unset", cfg[0])
	}
}

// Review: a total byte budget, so a big untracked directory costs one failed
// identification instead of a full re-read before and after every run.
func TestUntrackedByteBudget(t *testing.T) {
	old := maxUntrackedBytes
	maxUntrackedBytes = 1000
	t.Cleanup(func() { maxUntrackedBytes = old })
	dir := repoWithFiles(t)
	write(t, filepath.Join(dir, "data", "one.bin"), strings.Repeat("a", 400))
	write(t, filepath.Join(dir, "data", "two.bin"), strings.Repeat("b", 400))
	if id := workTreeID(t, dir); id.ID == "" || !id.Dirty {
		t.Fatalf("800 bytes under a 1000 byte budget: %+v", id)
	}
	write(t, filepath.Join(dir, "data", "three.bin"), strings.Repeat("c", 400))
	if id := workTreeID(t, dir); id.ID != "" || !id.Dirty {
		t.Fatalf("1200 bytes over a 1000 byte budget: %+v, want unknown", id)
	}
	if co, err := Identify(dir); err != nil || co.Tree != "" || !co.Dirty || co.Commit == "" {
		t.Fatalf("Identify over budget: %+v, %v", co, err)
	}
}

// Review: size+mtime alone is the same after cp -p, rsync -t, tar x, touch -r.
// The inode and change time make a rewrite visible.
func TestBigFileStampSeesRewritesThatKeepSizeAndMtime(t *testing.T) {
	old := maxHashedFileBytes
	maxHashedFileBytes = 100
	t.Cleanup(func() { maxHashedFileBytes = old })
	dir := repoWithFiles(t)
	big := filepath.Join(dir, "big.bin")
	mtime := time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)
	put := func(path string, b byte) {
		t.Helper()
		write(t, path, strings.Repeat(string(b), 200))
		if err := os.Chtimes(path, mtime, mtime); err != nil {
			t.Fatal(err)
		}
	}
	digest := func() string {
		t.Helper()
		r, err := untrackedDigest(dir)
		if err != nil || r.unknown || r.digest == "" {
			t.Fatalf("digest: %+v, %v", r, err)
		}
		return r.digest
	}
	put(big, 'a')
	first := digest()
	if again := digest(); again != first {
		t.Fatal("an untouched big file changed its digest")
	}
	time.Sleep(30 * time.Millisecond) // the kernel's change time has a coarse tick
	put(big, 'b')                     // in place, same size, same mtime
	second := digest()
	if second == first {
		t.Fatal("rewrite in place with the old mtime restored kept the digest")
	}
	time.Sleep(30 * time.Millisecond)
	tmp := filepath.Join(dir, "replacement")
	put(tmp, 'c') // a new inode with the same size and mtime, like cp -p
	if err := os.Rename(tmp, big); err != nil {
		t.Fatal(err)
	}
	if third := digest(); third == second || third == first {
		t.Fatal("replacing the file with a same-size, same-mtime copy kept the digest")
	}
}
