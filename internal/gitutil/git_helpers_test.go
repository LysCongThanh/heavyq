package gitutil

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func mkRepo(t *testing.T, dir string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) string {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	run("init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "f"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-q", "-m", "c")
	return run("rev-parse", "HEAD")
}

func TestSiblingPinning(t *testing.T) {
	base := t.TempDir()
	main := filepath.Join(base, "app")
	mkRepo(t, main)
	libSHA := mkRepo(t, filepath.Join(base, "lib"))

	s, err := ResolveSibling("lib", main)
	if err != nil || s.Name != "lib" || s.SHA != libSHA {
		t.Fatalf("ResolveSibling = %+v, %v", s, err)
	}
	if m := s.LocalMismatch(); m != "" {
		t.Fatalf("clean pinned sibling reported mismatch: %s", m)
	}
	if err := os.WriteFile(filepath.Join(base, "lib", "new"), []byte("y"), 0o644); err != nil {
		t.Fatal(err)
	}
	if m := s.LocalMismatch(); !strings.Contains(m, "uncommitted") {
		t.Fatalf("dirty sibling not detected: %q", m)
	}
	for _, bad := range []string{"app", "missing", "lib@nope", "@HEAD", "lib@-x"} {
		if _, err := ResolveSibling(bad, main); err == nil {
			t.Errorf("ResolveSibling(%q) should fail", bad)
		}
	}
	if IsClean(filepath.Join(base, "lib")) || !IsClean(main) {
		t.Fatal("IsClean wrong")
	}
}
