package gitutil

import (
	"crypto/sha256"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// gitIn runs git in dir (identity and signing pinned) and returns its output.
func gitIn(t testing.TB, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir, "-c", "commit.gpgsign=false"}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func write(t testing.TB, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// repoWithFiles makes a committed repo holding a.txt, sub/b.txt and .gitignore.
func repoWithFiles(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	gitIn(t, dir, "init", "-q", "-b", "main")
	write(t, filepath.Join(dir, "a.txt"), "alpha\n")
	write(t, filepath.Join(dir, "sub", "b.txt"), "beta\n")
	write(t, filepath.Join(dir, ".gitignore"), "ignored.log\nbuild/\n")
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-q", "-m", "init")
	return dir
}

func workTreeID(t *testing.T, dir string) TreeID {
	t.Helper()
	id, err := WorkTree(dir)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestCleanTreeIdentityIsTheHeadTreeTheCacheUses(t *testing.T) {
	dir := repoWithFiles(t)
	head, err := TreeOf(dir, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if id := workTreeID(t, dir); id.ID != head || id.Dirty {
		t.Fatalf("clean tree: got %+v, want {%s false}", id, head)
	}
	if got, err := CleanTree(dir); err != nil || got != head {
		t.Fatalf("CleanTree = %q, %v", got, err)
	}
	// An ignored file does not make the tree dirty or change its identity.
	write(t, filepath.Join(dir, "ignored.log"), "noise")
	write(t, filepath.Join(dir, "build", "out.bin"), "noise")
	if id := workTreeID(t, dir); id.ID != head || id.Dirty {
		t.Fatalf("ignored files changed the identity: %+v", id)
	}
	write(t, filepath.Join(dir, "a.txt"), "edited\n")
	if _, err := CleanTree(dir); err == nil || !strings.Contains(err.Error(), "uncommitted") {
		t.Fatalf("CleanTree on a dirty tree: %v", err)
	}
}

func TestDirtyTreeIdentity(t *testing.T) {
	dir := repoWithFiles(t)
	head := workTreeID(t, dir).ID
	a, b := filepath.Join(dir, "a.txt"), filepath.Join(dir, "sub", "b.txt")

	// A tracked edit: dirty, anchored on the HEAD tree, deterministic.
	write(t, a, "edit one\n")
	one := workTreeID(t, dir)
	if !one.Dirty || !strings.HasPrefix(one.ID, head+"+") || len(one.ID) != len(head)+1+digestLen {
		t.Fatalf("dirty id %+v does not extend %s", one, head)
	}
	if again := workTreeID(t, dir); again != one {
		t.Fatalf("same edits, different id: %+v vs %+v", again, one)
	}

	// Staging the same content is the same files: same identity.
	gitIn(t, dir, "add", "a.txt")
	if staged := workTreeID(t, dir); staged != one {
		t.Fatalf("staging changed the identity: %+v vs %+v", staged, one)
	}
	// An unstaged edit on top of a staged one changes it.
	write(t, a, "edit two\n")
	two := workTreeID(t, dir)
	if two.ID == one.ID {
		t.Fatal("an edit on top of a staged edit kept the id")
	}
	// A second file, a deletion and a mode change each change it.
	seen := map[string]string{one.ID: "one", two.ID: "two"}
	step := func(name string) {
		t.Helper()
		id := workTreeID(t, dir)
		if prev, dup := seen[id.ID]; dup {
			t.Fatalf("%s has the same id as %s", name, prev)
		}
		seen[id.ID] = name
	}
	write(t, b, "beta changed\n")
	step("second file edited")
	if err := os.Chmod(b, 0o755); err != nil {
		t.Fatal(err)
	}
	step("mode change")
	if err := os.Remove(b); err != nil {
		t.Fatal(err)
	}
	step("deletion")
	// Reverting every change restores exactly the clean id.
	gitIn(t, dir, "checkout", "HEAD", "--", ".")
	if id := workTreeID(t, dir); id.ID != head || id.Dirty {
		t.Fatalf("revert did not restore the clean id: %+v want %s", id, head)
	}
}

func TestUntrackedFilesAreIdentifiedByNameAndContent(t *testing.T) {
	dir := repoWithFiles(t)
	head := workTreeID(t, dir).ID
	n := filepath.Join(dir, "new", "n.txt")

	write(t, n, "x")
	x := workTreeID(t, dir)
	if !x.Dirty || x.ID == head {
		t.Fatalf("an untracked file must make the tree dirty: %+v", x)
	}
	write(t, n, "y")
	y := workTreeID(t, dir)
	if y.ID == x.ID {
		t.Fatal("changed untracked content kept the id")
	}
	write(t, n, "x")
	if back := workTreeID(t, dir); back != x {
		t.Fatalf("same untracked content, different id: %+v vs %+v", back, x)
	}
	if err := os.Rename(n, filepath.Join(dir, "new", "m.txt")); err != nil {
		t.Fatal(err)
	}
	if renamed := workTreeID(t, dir); renamed.ID == x.ID {
		t.Fatal("renaming an untracked file kept the id")
	}
	// A symlink is identified by its target, not followed.
	if err := os.Remove(filepath.Join(dir, "new", "m.txt")); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "lnk")
	byTarget := map[string]bool{}
	for _, target := range []string{"a.txt", "sub/b.txt"} {
		_ = os.Remove(link)
		if err := os.Symlink(target, link); err != nil {
			t.Fatal(err)
		}
		id := workTreeID(t, dir).ID
		if byTarget[id] {
			t.Fatal("retargeting a symlink kept the id")
		}
		byTarget[id] = true
	}
	_ = os.Remove(link)
	_ = os.RemoveAll(filepath.Join(dir, "new"))
	if id := workTreeID(t, dir); id.ID != head {
		t.Fatalf("removing the untracked files did not restore the clean id: %+v", id)
	}
}

func TestBinaryEditsChangeTheIdentity(t *testing.T) {
	dir := repoWithFiles(t)
	bin := filepath.Join(dir, "data.bin")
	write(t, bin, "\x00\x01\x02")
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-q", "-m", "bin")
	write(t, bin, "\x00\x01\x03")
	first := workTreeID(t, dir)
	write(t, bin, "\x00\x01\x04")
	if second := workTreeID(t, dir); second.ID == first.ID || !second.Dirty {
		t.Fatal("a binary edit did not change the identity")
	}
}

func TestHugeUntrackedFileIsIdentifiedBySizeAndTime(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "huge")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(maxHashedFileBytes + 1); err != nil { // sparse: no real I/O
		t.Fatal(err)
	}
	f.Close()
	kind, sum := digestEntry(sha256.New(), make([]byte, 4096), p)
	if kind != 'B' || len(sum) != 32 {
		t.Fatalf("kind %c, %d bytes", kind, len(sum))
	}
	if kind, _ := digestEntry(sha256.New(), nil, filepath.Join(dir, "missing")); kind != 'G' {
		t.Fatalf("a vanished file must be marked gone, got %c", kind)
	}
	if kind, _ := digestEntry(sha256.New(), nil, dir); kind != 'O' {
		t.Fatalf("a directory must be 'other', got %c", kind)
	}
}

func TestIdentifyAndLocateAcrossWorktrees(t *testing.T) {
	dir := repoWithFiles(t)
	if real, err := filepath.EvalSymlinks(dir); err == nil {
		dir = real
	}
	head := gitIn(t, dir, "rev-parse", "HEAD")
	tree := gitIn(t, dir, "rev-parse", "HEAD^{tree}")

	co, err := Identify(dir)
	if err != nil {
		t.Fatal(err)
	}
	if co.Worktree != dir || co.Subdir != "" || co.Commit != head || co.Tree != tree || co.Dirty || len(co.Repo) != repoIDLen {
		t.Fatalf("Identify at the root: %+v", co)
	}
	sub, err := Identify(filepath.Join(dir, "sub"))
	if err != nil || sub.Subdir != "sub" || sub.Worktree != dir || sub.Repo != co.Repo || sub.Tree != tree {
		t.Fatalf("Identify in a subdirectory: %+v, %v", sub, err)
	}

	// A linked work tree shares the repo id but not the path.
	linked := filepath.Join(t.TempDir(), "wt")
	gitIn(t, dir, "worktree", "add", "-q", "-b", "other", linked)
	if real, err := filepath.EvalSymlinks(linked); err == nil {
		linked = real
	}
	lc, err := Identify(linked)
	if err != nil {
		t.Fatal(err)
	}
	if lc.Repo != co.Repo || lc.Worktree != linked || lc.Commit != head || lc.Tree != tree {
		t.Fatalf("linked work tree %+v vs main %+v", lc, co)
	}
	// Edits in one work tree do not leak into the other's identity.
	write(t, filepath.Join(linked, "a.txt"), "linked edit\n")
	if lc, _ = Identify(linked); !lc.Dirty || lc.Tree == tree {
		t.Fatalf("linked tree should be dirty: %+v", lc)
	}
	if mc, _ := Identify(dir); mc.Dirty || mc.Tree != tree {
		t.Fatalf("main tree must stay clean: %+v", mc)
	}
	other := repoWithFiles(t)
	if oc, _ := Locate(other); oc.Repo == co.Repo || oc.Repo == "" {
		t.Fatalf("different repos share an id: %q", oc.Repo)
	}
}

func TestIdentifyOutsideRepoAndBeforeFirstCommit(t *testing.T) {
	if co, err := Identify(t.TempDir()); err == nil || co != (Checkout{}) {
		t.Fatalf("outside a repo: %+v, %v", co, err)
	}
	dir := t.TempDir()
	gitIn(t, dir, "init", "-q", "-b", "main")
	write(t, filepath.Join(dir, "f"), "x")
	co, err := Identify(dir)
	if err != nil || co.Repo == "" || co.Commit != "" || co.Tree != "" || !co.Dirty {
		t.Fatalf("unborn HEAD: %+v, %v", co, err)
	}
}

func TestPinnedIgnoresTheLocalWorkTree(t *testing.T) {
	dir := repoWithFiles(t)
	first := gitIn(t, dir, "rev-parse", "HEAD")
	write(t, filepath.Join(dir, "a.txt"), "second\n")
	gitIn(t, dir, "commit", "-q", "-am", "second")
	write(t, filepath.Join(dir, "a.txt"), "dirty now\n")

	co, err := Pinned(dir, first)
	if err != nil {
		t.Fatal(err)
	}
	if co.Commit != first || co.Tree != gitIn(t, dir, "rev-parse", first+"^{tree}") || co.Dirty || co.Repo == "" {
		t.Fatalf("Pinned: %+v", co)
	}
	if _, err := Pinned(dir, "nonexistent"); err == nil {
		t.Fatal("an unknown commit must fail")
	}
}
