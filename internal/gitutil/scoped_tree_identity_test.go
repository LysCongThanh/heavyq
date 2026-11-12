package gitutil

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// repoForScopes holds Go code, a go.mod, docs and the anchor file, committed.
func repoForScopes(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	gitIn(t, dir, "init", "-q", "-b", "main")
	write(t, filepath.Join(dir, "main.go"), "package main\n")
	write(t, filepath.Join(dir, "pkg", "lib.go"), "package pkg\n")
	write(t, filepath.Join(dir, "go.mod"), "module m\n")
	write(t, filepath.Join(dir, "docs", "guide.md"), "# guide\n")
	write(t, filepath.Join(dir, "README.md"), "# readme\n")
	write(t, filepath.Join(dir, ".heavyq", "anchor.md"), "- [ ] one\n")
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-q", "-m", "init")
	return dir
}

var goScope = []string{"**/*.go", "go.mod"}

func scoped(t *testing.T, dir string, paths []string) Checkout {
	t.Helper()
	co, err := IdentifyScoped(dir, paths)
	if err != nil {
		t.Fatalf("IdentifyScoped(%v): %v", paths, err)
	}
	return co
}

// A commit that changes no file of the scope keeps the scoped id; one that changes a file of it does not. The
// whole tree id moves with both, and the two kinds of id never look alike.
func TestScopedIdentitySurvivesCommitsOutsideTheScope(t *testing.T) {
	dir := repoForScopes(t)
	first := scoped(t, dir, goScope)
	whole := mustID(t, dir)
	if !strings.HasPrefix(first.Tree, ScopedIDPrefix) || first.Tree == whole.ID || first.Dirty {
		t.Fatalf("scoped id %q (dirty %v), whole %q", first.Tree, first.Dirty, whole.ID)
	}

	write(t, filepath.Join(dir, "docs", "guide.md"), "# guide, edited\n")
	write(t, filepath.Join(dir, "CHANGELOG.md"), "new\n")
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-q", "-m", "docs only")
	if after := scoped(t, dir, goScope); after.Tree != first.Tree || after.Dirty || after.Commit == first.Commit {
		t.Fatalf("a docs-only commit changed the scoped id: %+v -> %+v", first, after)
	}
	if mustID(t, dir).ID == whole.ID {
		t.Fatal("the whole tree id should have moved with the docs")
	}

	write(t, filepath.Join(dir, "pkg", "lib.go"), "package pkg\n\nvar X = 1\n")
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-q", "-m", "code")
	if after := scoped(t, dir, goScope); after.Tree == first.Tree {
		t.Fatalf("a code commit kept the scoped id %q", after.Tree)
	}
}

// Edits to files outside the scope leave the work tree clean for it; an edit, a new file or a deleted file
// inside it makes it dirty with an id of its own, and undoing the edit restores the clean one.
func TestScopedIdentityOfTheWorkTree(t *testing.T) {
	dir := repoForScopes(t)
	clean := scoped(t, dir, goScope)

	write(t, filepath.Join(dir, "docs", "guide.md"), "edited\n")
	write(t, filepath.Join(dir, "notes.txt"), "untracked\n")
	write(t, filepath.Join(dir, ".heavyq", "anchor.md"), "- [x] one\n")
	if got := scoped(t, dir, goScope); got.Tree != clean.Tree || got.Dirty {
		t.Fatalf("edits outside the scope changed the identity: %+v, want %q clean", got, clean.Tree)
	}
	if whole := mustID(t, dir); !whole.Dirty {
		t.Fatal("the whole tree should be dirty")
	}

	main := filepath.Join(dir, "main.go")
	write(t, main, "package main\n\nfunc main() {}\n")
	edited := scoped(t, dir, goScope)
	if !edited.Dirty || !strings.HasPrefix(edited.Tree, clean.Tree+"+") {
		t.Fatalf("an edit inside the scope: %+v, want %q+digest", edited, clean.Tree)
	}
	write(t, main, "package main\n\nfunc main() { println() }\n")
	if other := scoped(t, dir, goScope); other.Tree == edited.Tree {
		t.Fatal("two different edits got the same id")
	}
	write(t, main, "package main\n")
	if back := scoped(t, dir, goScope); back.Tree != clean.Tree || back.Dirty {
		t.Fatalf("undoing the edit: %+v, want %q clean", back, clean.Tree)
	}

	write(t, filepath.Join(dir, "extra.go"), "package main\n")
	if got := scoped(t, dir, goScope); !got.Dirty || got.Tree == clean.Tree {
		t.Fatalf("a new Go file: %+v", got)
	}
	gitIn(t, dir, "clean", "-fdq", "extra.go")
	if err := removeFile(filepath.Join(dir, "pkg", "lib.go")); err != nil {
		t.Fatal(err)
	}
	if got := scoped(t, dir, goScope); !got.Dirty || got.Tree == clean.Tree {
		t.Fatalf("a deleted Go file: %+v", got)
	}
}

// The patterns are part of the id (a wider scope is another proof), their order is not, and a scope that names
// no file at all has no identity instead of one that every code shares.
func TestScopedIdentityDependsOnThePatternsNotTheirOrder(t *testing.T) {
	dir := repoForScopes(t)
	a := scoped(t, dir, []string{"**/*.go", "go.mod"})
	if b := scoped(t, dir, []string{"go.mod", "**/*.go"}); b.Tree != a.Tree {
		t.Fatalf("pattern order changed the id: %q vs %q", a.Tree, b.Tree)
	}
	if c := scoped(t, dir, []string{"**/*.go", "go.mod", "**/*.md"}); c.Tree == a.Tree {
		t.Fatal("a wider scope kept the id")
	}
	for _, bad := range [][]string{{"**/*.rs"}, {"nonexistent/**"}} {
		co, err := IdentifyScoped(dir, bad)
		if !errors.Is(err, ErrScopeEmpty) || co.Tree != "" {
			t.Errorf("paths %v matching nothing: tree %q, err %v", bad, co.Tree, err)
		}
	}
	for _, bad := range [][]string{{"/etc/passwd"}, {"../x"}, {"a/[b]"}, {"src/"}, {""}, {"a**b"}} {
		if co, err := IdentifyScoped(dir, bad); err == nil || co.Tree != "" {
			t.Errorf("invalid paths %v were accepted: tree %q", bad, co.Tree)
		}
	}
}

// What a remote runs is the shipped commit: its scoped id is the one the work tree has when it is clean, and the
// anchor file or the docs never count.
func TestPinnedScopedMatchesACleanWorkTree(t *testing.T) {
	dir := repoForScopes(t)
	head := mustID(t, dir)
	co := scoped(t, dir, goScope)
	pin, err := PinnedScoped(dir, co.Commit, goScope)
	if err != nil || pin.Tree != co.Tree || pin.Dirty || pin.Commit != co.Commit {
		t.Fatalf("PinnedScoped = %+v, %v; want the work tree's %+v", pin, err, co)
	}
	whole, err := PinnedScoped(dir, co.Commit, nil)
	if err != nil || whole.Tree != head.ID {
		t.Fatalf("no paths is the whole tree: %+v, %v (want %q)", whole, err, head.ID)
	}
}

// The scope is git's own glob: the same files whether git or the validator reads the pattern. A table over the
// documented shapes, against `git ls-files` in a real repository.
func TestScopePatternsMeanWhatTheDocsSay(t *testing.T) {
	dir := repoForScopes(t)
	write(t, filepath.Join(dir, "pkg", "deep", "x.go"), "package deep\n")
	write(t, filepath.Join(dir, "docs", "api", "v1.md"), "api\n")
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-q", "-m", "more")
	cases := []struct {
		pattern string
		want    string
	}{
		{"**/*.go", "main.go pkg/deep/x.go pkg/lib.go"},
		{"*.go", "main.go"},
		{"pkg/*.go", "pkg/lib.go"},
		{"pkg/**", "pkg/deep/x.go pkg/lib.go"},
		{"pkg", "pkg/deep/x.go pkg/lib.go"},
		{"docs/**/*.md", "docs/api/v1.md docs/guide.md"},
		{"go.mod", "go.mod"},
		{"?ain.go", "main.go"},
		{"**/lib.go", "pkg/lib.go"},
	}
	for _, c := range cases {
		out := gitIn(t, dir, "ls-files", "--", ":(top,glob)"+c.pattern)
		if got := strings.Join(strings.Fields(out), " "); got != c.want {
			t.Errorf("pattern %q matches %q, want %q", c.pattern, got, c.want)
		}
		// the pattern is also accepted as a scope (it matches at least one committed file)
		if _, err := IdentifyScoped(dir, []string{c.pattern}); err != nil {
			t.Errorf("pattern %q: %v", c.pattern, err)
		}
	}
}

func removeFile(path string) error { return os.Remove(path) }

// A repository with the sha256 object format has another empty tree id: the scoped id is built the same way.
func TestScopedIdentityWithSha256Repository(t *testing.T) {
	dir := t.TempDir()
	if out, err := gitOut(dir, "init", "-q", "-b", "main", "--object-format=sha256"); err != nil {
		t.Skipf("git without sha256 repositories: %v %s", err, out)
	}
	write(t, filepath.Join(dir, "main.go"), "package main\n")
	write(t, filepath.Join(dir, "README.md"), "# readme\n")
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-q", "-m", "one")
	first := scoped(t, dir, goScope[:1])
	if len(first.Commit) != 64 || !strings.HasPrefix(first.Tree, ScopedIDPrefix) {
		t.Fatalf("sha256 repo: %+v", first)
	}
	write(t, filepath.Join(dir, "README.md"), "# readme, edited\n")
	gitIn(t, dir, "commit", "-qam", "docs")
	if after := scoped(t, dir, goScope[:1]); after.Tree != first.Tree {
		t.Fatalf("a docs-only commit changed the scoped id: %q -> %q", first.Tree, after.Tree)
	}
}
