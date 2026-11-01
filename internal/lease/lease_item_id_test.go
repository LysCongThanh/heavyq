package lease

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateItem(t *testing.T) {
	ok := []string{"a", "task-1", "plans/foo/phase-01", "a.b_c-d", "0x", strings.Repeat("a", 128)}
	bad := []string{
		"", "A", "-a", ".a", "_a", "/a", "a/", "a//b", "a/../b", "../etc/passwd", "..", "a/./b",
		"a/..", "a b", "a\x00b", "a\nb", "a\\b", "a+b", "é", strings.Repeat("a", 129),
		"/etc/passwd", "a/b/../../../x",
	}
	for _, id := range ok {
		if err := ValidateItem(id); err != nil {
			t.Errorf("%q rejected: %v", id, err)
		}
	}
	for _, id := range bad {
		if err := ValidateItem(id); err == nil {
			t.Errorf("%q accepted", id)
		}
	}
}

func TestFileNameIsInjectiveAndFlat(t *testing.T) {
	seen := map[string]string{}
	for _, id := range []string{"a/b", "a.b", "a-b", "ab", "a/b/c", "a/bc"} {
		n := fileName(id)
		if strings.ContainsRune(n, '/') {
			t.Errorf("%q -> %q has a separator", id, n)
		}
		if prev, dup := seen[n]; dup {
			t.Errorf("%q and %q collide on %q", prev, id, n)
		}
		seen[n] = id
	}
}

// Escape attempts never create or touch anything outside the leases dir.
func TestPathEscapeRejected(t *testing.T) {
	root := t.TempDir()
	common := filepath.Join(root, "common")
	if err := os.Mkdir(common, 0o700); err != nil {
		t.Fatal(err)
	}
	s, err := Open(common)
	if err != nil {
		t.Fatal(err)
	}
	o := owner(t, "laneA", "s1")
	for _, id := range []string{"../escape", "../../escape", "a/../../escape", "/abs", "..", "x/.."} {
		if _, err := s.Claim(id, o); err == nil {
			t.Errorf("Claim(%q) accepted", id)
		}
		if _, err := s.Renew(id, o); err == nil {
			t.Errorf("Renew(%q) accepted", id)
		}
		if err := s.Release(id, o, true); err == nil {
			t.Errorf("Release(%q) accepted", id)
		}
	}
	var found []string
	_ = filepath.WalkDir(root, func(p string, d os.DirEntry, _ error) error {
		if strings.Contains(filepath.Base(p), "escape") {
			found = append(found, p)
		}
		return nil
	})
	if len(found) > 0 {
		t.Fatalf("escape files created: %v", found)
	}
}
