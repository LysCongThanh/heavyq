package gitutil

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveSiblingByPathAndLocation(t *testing.T) {
	base := t.TempDir()
	main := filepath.Join(base, "app")
	mkRepo(t, main)
	libSHA := mkRepo(t, filepath.Join(base, "lib"))
	elsewhere := filepath.Join(base, "deeper", "tools")
	toolsSHA := mkRepo(t, elsewhere)

	// By bare name and by path: the same repository, next to the main one.
	for _, spec := range []string{"lib", filepath.Join(base, "lib"), "../lib@HEAD"} {
		if spec == "../lib@HEAD" {
			t.Chdir(main)
		}
		s, err := ResolveSibling(spec, main)
		if err != nil || s.SHA != libSHA || !s.NextToMain || s.Label() != "lib@"+libSHA[:12] {
			t.Fatalf("ResolveSibling(%q) = %+v, %v", spec, s, err)
		}
	}
	// A repository elsewhere keeps its name (it is recreated as ../tools remotely),
	// but locally ../tools is another directory: never eligible to run in place.
	s, err := ResolveSibling(elsewhere, main)
	if err != nil || s.Name != "tools" || s.SHA != toolsSHA || s.NextToMain {
		t.Fatalf("ResolveSibling(elsewhere) = %+v, %v", s, err)
	}
	if m := s.LocalMismatch(); !strings.Contains(m, "not next to the main repository") {
		t.Fatalf("LocalMismatch = %q", m)
	}
}

func TestResolveSiblingRefusals(t *testing.T) {
	base := t.TempDir()
	main := filepath.Join(base, "app")
	mkRepo(t, main)
	mkRepo(t, filepath.Join(base, "lib"))
	if err := os.MkdirAll(filepath.Join(base, "lib", "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(base, "plain"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(main, filepath.Join(base, "alias")); err != nil {
		t.Skip("no symlinks:", err)
	}
	for spec, want := range map[string]string{
		"gone":                            "not an existing directory",
		"plain":                           "not a git repository",
		"lib@nope":                        "no commit",
		"alias":                           "main repository", // same path under another name
		filepath.Join(base, "lib", "sub"): "not its root",
	} {
		_, err := ResolveSibling(spec, main)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("ResolveSibling(%q) error = %v, want %q", spec, err, want)
		}
	}
	if _, err := ResolveSibling("lib", ""); err == nil {
		t.Error("a bare name without a main repository must be refused")
	}
}

func TestSiblingLocalMismatchHeadMoved(t *testing.T) {
	base := t.TempDir()
	main := filepath.Join(base, "app")
	mkRepo(t, main)
	lib := filepath.Join(base, "lib")
	mkRepo(t, lib)
	s, err := ResolveSibling("lib", main)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(lib, "f"), []byte("y"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", "."}, {"commit", "-q", "-m", "two"}} {
		cmd := exec.Command("git", append([]string{"-C", lib}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	if m := s.LocalMismatch(); !strings.Contains(m, "HEAD differs from pinned") {
		t.Fatalf("LocalMismatch = %q", m)
	}
}
