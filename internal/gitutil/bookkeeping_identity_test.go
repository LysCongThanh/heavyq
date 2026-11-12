package gitutil

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const anchorTodo = "# goal\n\n- [ ] ship it\n"
const anchorDone = "# goal\n\n- [x] ship it\n"

// repoWithBookkeeping is repoWithFiles plus heavyq.toml and the bookkeeping
// heavyq writes: the anchor, a decision and a stance.
func repoWithBookkeeping(t *testing.T) string {
	t.Helper()
	dir := repoWithFiles(t)
	write(t, filepath.Join(dir, "heavyq.toml"), "[gate.ready]\ncommands = [\"true\"]\n")
	write(t, filepath.Join(dir, ".heavyq", "anchor.md"), anchorTodo)
	write(t, filepath.Join(dir, ".heavyq", "decisions", "d1.md"), "chose A\n")
	write(t, filepath.Join(dir, ".heavyq", "stances", "s1.md"), "stance\n")
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-q", "-m", "bookkeeping")
	return dir
}

// gitOut runs git in dir and returns its combined output and error.
func gitOut(dir string, args ...string) (string, error) {
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	return string(out), err
}

func mustID(t *testing.T, dir string) TreeID {
	t.Helper()
	return workTreeID(t, dir)
}

func TestBookkeepingEditsDoNotChangeTheIdentity(t *testing.T) {
	dir := repoWithBookkeeping(t)
	base := mustID(t, dir)
	if base.Dirty || base.ID == "" {
		t.Fatalf("base: %+v", base)
	}

	// Tick the box (uncommitted), add a decision, rewrite a stance.
	write(t, filepath.Join(dir, ".heavyq", "anchor.md"), anchorDone)
	write(t, filepath.Join(dir, ".heavyq", "decisions", "d2.md"), "chose B\n")
	write(t, filepath.Join(dir, ".heavyq", "stances", "s1.md"), "another stance\n")
	if got := mustID(t, dir); got != base {
		t.Fatalf("bookkeeping edits changed the identity: %+v, want %+v", got, base)
	}
	if tree, err := CleanTree(dir); err != nil || tree != base.ID {
		t.Fatalf("CleanTree = %q, %v; want %q (bookkeeping alone is not dirt)", tree, err, base.ID)
	}

	// Committing them changes HEAD's tree but not the identity.
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-q", "-m", "tick")
	if got := mustID(t, dir); got != base {
		t.Fatalf("committed bookkeeping changed the identity: %+v, want %+v", got, base)
	}
	co, err := Identify(dir)
	if err != nil || co.Tree != base.ID || co.Dirty {
		t.Fatalf("Identify = %+v, %v", co, err)
	}
}

func TestIdentityKeepsEverythingElse(t *testing.T) {
	dir := repoWithBookkeeping(t)
	base := mustID(t, dir)
	for name, edit := range map[string]func(){
		"code":           func() { write(t, filepath.Join(dir, "a.txt"), "edited\n") },
		"heavyq.toml":    func() { write(t, filepath.Join(dir, "heavyq.toml"), "[gate.ready]\ncommands = [\"false\"]\n") },
		"other heavyq/":  func() { write(t, filepath.Join(dir, ".heavyq", "notes.md"), "n\n") },
		"anchor sibling": func() { write(t, filepath.Join(dir, ".heavyq", "anchor.md.bak"), "n\n") },
		"decisions name": func() { write(t, filepath.Join(dir, ".heavyq", "decisions.md"), "n\n") },
		"untracked code": func() { write(t, filepath.Join(dir, "new.go"), "package x\n") },
	} {
		edit()
		got := mustID(t, dir)
		if got.ID == "" || got.ID == base.ID || !got.Dirty {
			t.Fatalf("%s: edit did not change the identity: %+v (base %+v)", name, got, base)
		}
		gitIn(t, dir, "add", ".")
		gitIn(t, dir, "commit", "-q", "-m", name)
		if c := mustID(t, dir); c.ID == base.ID || c.Dirty {
			t.Fatalf("%s: committed edit kept the identity: %+v", name, c)
		}
		gitIn(t, dir, "reset", "-q", "--hard", "HEAD~1")
	}
}

func TestBookkeepingEditsLeaveCodeEditsIdentifiedTheSame(t *testing.T) {
	dir := repoWithBookkeeping(t)
	write(t, filepath.Join(dir, "a.txt"), "edited\n")
	write(t, filepath.Join(dir, "scratch.txt"), "x\n")
	withCode := mustID(t, dir)
	if !withCode.Dirty {
		t.Fatalf("expected dirty: %+v", withCode)
	}
	write(t, filepath.Join(dir, ".heavyq", "anchor.md"), anchorDone)
	write(t, filepath.Join(dir, ".heavyq", "decisions", "new.md"), "d\n")
	if got := mustID(t, dir); got != withCode {
		t.Fatalf("a tick changed a dirty identity: %+v, want %+v", got, withCode)
	}
}

// The identity of (HEAD = X, dirty only in bookkeeping) equals the identity of
// (HEAD = X', X' differs from X only in bookkeeping), clean or dirty, and a
// repository that never had bookkeeping files is the same code too.
func TestCleanAndDirtyBookkeepingAreConsistent(t *testing.T) {
	dir := repoWithBookkeeping(t)
	x := mustID(t, dir)
	write(t, filepath.Join(dir, "a.txt"), "edited\n")
	xDirty := mustID(t, dir)
	gitIn(t, dir, "checkout", "-q", "--", "a.txt")

	// X': a committed tick, a removed decision, a new stance.
	write(t, filepath.Join(dir, ".heavyq", "anchor.md"), anchorDone)
	gitIn(t, dir, "rm", "-q", ".heavyq/decisions/d1.md")
	write(t, filepath.Join(dir, ".heavyq", "stances", "s2.md"), "s\n")
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-q", "-m", "X'")
	if got := mustID(t, dir); got != x {
		t.Fatalf("X' = %+v, X = %+v", got, x)
	}
	write(t, filepath.Join(dir, "a.txt"), "edited\n")
	if got := mustID(t, dir); got != xDirty {
		t.Fatalf("dirty on X' = %+v, dirty on X = %+v", got, xDirty)
	}
	gitIn(t, dir, "checkout", "-q", "--", "a.txt")

	// No bookkeeping at all: the .heavyq directory vanishes from the tree, as
	// an empty directory does in git. The identity is that of the real tree.
	gitIn(t, dir, "rm", "-rq", ".heavyq")
	gitIn(t, dir, "commit", "-q", "-m", "no bookkeeping")
	if got := mustID(t, dir); got != x {
		t.Fatalf("without bookkeeping = %+v, X = %+v", got, x)
	}
}

// effectiveTreeOf must equal the id git gives the tree without the files.
func TestEffectiveTreeIsTheTreeGitWouldBuild(t *testing.T) {
	dir := repoWithBookkeeping(t)
	withBook, err := effectiveTreeOf(dir, "HEAD", bookkeepingPaths(dir, ""))
	if err != nil {
		t.Fatal(err)
	}
	if head, _ := TreeOf(dir, "HEAD"); withBook == head {
		t.Fatalf("the effective tree equals HEAD's tree %s: nothing was removed", head)
	}
	gitIn(t, dir, "rm", "-rq", ".heavyq")
	gitIn(t, dir, "commit", "-q", "-m", "stripped")
	want, err := TreeOf(dir, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if withBook != want {
		t.Fatalf("effective tree %s, git's tree without the files %s", withBook, want)
	}
	// With nothing to remove the answer is HEAD's own tree.
	if got, err := effectiveTreeOf(dir, "HEAD", bookkeepingPaths(dir, "")); err != nil || got != want {
		t.Fatalf("fast path = %q, %v; want %s", got, err, want)
	}
}

func TestEffectiveTreeWithSha256Repository(t *testing.T) {
	dir := t.TempDir()
	if out, err := gitOut(dir, "init", "-q", "-b", "main", "--object-format=sha256"); err != nil {
		t.Skipf("git without sha256 repositories: %v %s", err, out)
	}
	write(t, filepath.Join(dir, "a.txt"), "a\n")
	write(t, filepath.Join(dir, ".heavyq", "anchor.md"), anchorTodo)
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-q", "-m", "one")
	got, err := effectiveTreeOf(dir, "HEAD", bookkeepingPaths(dir, ""))
	if err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "rm", "-rq", ".heavyq")
	gitIn(t, dir, "commit", "-q", "-m", "two")
	if want, _ := TreeOf(dir, "HEAD"); got != want || len(got) != 64 {
		t.Fatalf("sha256 repo: effective %s, git %s", got, want)
	}
}

func TestConfiguredAnchorFileIsExcludedInsteadOfTheDefault(t *testing.T) {
	dir := repoWithFiles(t)
	write(t, filepath.Join(dir, "heavyq.toml"), "[anchor]\nfile = \"docs/./TODO.md\"\n")
	write(t, filepath.Join(dir, "docs", "TODO.md"), anchorTodo)
	write(t, filepath.Join(dir, "docs", "guide.md"), "guide\n")
	write(t, filepath.Join(dir, ".heavyq", "anchor.md"), anchorTodo)
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-q", "-m", "custom anchor")
	base := mustID(t, dir)

	write(t, filepath.Join(dir, "docs", "TODO.md"), anchorDone)
	if got := mustID(t, dir); got != base {
		t.Fatalf("ticking the configured anchor changed the identity: %+v", got)
	}
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-q", "-m", "tick")
	if got := mustID(t, dir); got != base {
		t.Fatalf("committed tick: %+v", got)
	}
	write(t, filepath.Join(dir, "docs", "guide.md"), "changed\n")
	if got := mustID(t, dir); got.ID == base.ID {
		t.Fatalf("a sibling of the anchor is code: %+v", got)
	}
	gitIn(t, dir, "checkout", "-q", "--", "docs/guide.md")
	// The default path is an ordinary file once another anchor is configured.
	write(t, filepath.Join(dir, ".heavyq", "anchor.md"), anchorDone)
	if got := mustID(t, dir); got.ID == base.ID {
		t.Fatalf("the unconfigured .heavyq/anchor.md is still ignored: %+v", got)
	}
}

func TestBookkeepingPathsFallBackAndNeverHideTheConfig(t *testing.T) {
	def := []string{".heavyq/anchor.md", ".heavyq/decisions", ".heavyq/stances"}
	for name, toml := range map[string]string{
		"absent":         "",
		"invalid toml":   "[anchor\nfile = ",
		"wrong type":     "[anchor]\nfile = 3\n",
		"escaping path":  "[anchor]\nfile = \"../x.md\"\n",
		"absolute path":  "[anchor]\nfile = \"/etc/x.md\"\n",
		"the config":     "[anchor]\nfile = \"heavyq.toml\"\n",
		"empty":          "[anchor]\nfile = \"\"\n",
		"other sections": "[queue]\nmax = 1\n",
		"a directory":    "[anchor]\nfile = \"src\"\n",
		"trailing slash": "[anchor]\nfile = \"plan.md/\"\n",
		"a code file":    "[anchor]\nfile = \"main.go\"\n",
		"the root":       "[anchor]\nfile = \".\"\n",
		"control chars":  "[anchor]\nfile = \"a\\nb.md\"\n",
	} {
		dir := t.TempDir()
		if toml != "" {
			write(t, filepath.Join(dir, "heavyq.toml"), toml)
		}
		if got := strings.Join(bookkeepingPaths(dir, ""), ","); got != strings.Join(def, ",") {
			t.Errorf("%s: %s, want %s", name, got, strings.Join(def, ","))
		}
	}
}

func TestPinnedUsesTheCommitsConfigAndIgnoresBookkeeping(t *testing.T) {
	dir := repoWithBookkeeping(t)
	before := mustID(t, dir)
	first, _ := ResolveCommit(dir, "HEAD")
	write(t, filepath.Join(dir, ".heavyq", "anchor.md"), anchorDone)
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-q", "-m", "tick")
	second, _ := ResolveCommit(dir, "HEAD")
	a, err := Pinned(dir, first)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Pinned(dir, second)
	if err != nil {
		t.Fatal(err)
	}
	if a.Tree != before.ID || b.Tree != before.ID {
		t.Fatalf("pinned trees %s, %s; want %s", a.Tree, b.Tree, before.ID)
	}
}
