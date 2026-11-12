package hook

import (
	"os"
	"path/filepath"
	"testing"
)

// A settings.json that is a symlink (a dotfiles checkout) stays a symlink:
// the new content lands in the link's target.
func TestWriteSettingsKeepsSymlink(t *testing.T) {
	root := t.TempDir()
	dotfiles := filepath.Join(root, "dotfiles", "claude")
	if err := os.MkdirAll(dotfiles, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dotfiles, "settings.json")
	if err := os.WriteFile(target, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "home", ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if err := WriteSettings(link, []byte(`{"a":1}`)); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Lstat(link); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("the symlink was replaced: %v %v", fi, err)
	}
	if b, _ := os.ReadFile(target); string(b) != `{"a":1}` {
		t.Fatalf("target = %q", b)
	}
	if fi, _ := os.Stat(target); fi.Mode().Perm() != 0o600 {
		t.Fatalf("target mode = %v", fi.Mode().Perm())
	}
	if bak, _ := os.ReadFile(link + ".bak"); string(bak) != "{}\n" {
		t.Fatalf("backup = %q", bak)
	}
	// A dangling link is refused, never replaced by a regular file.
	dangling := filepath.Join(root, "home", ".claude", "settings.local.json")
	if err := os.Symlink(filepath.Join(root, "gone", "settings.json"), dangling); err != nil {
		t.Fatal(err)
	}
	if err := WriteSettings(dangling, []byte(`{}`)); err == nil {
		t.Fatal("a dangling symlink was written through")
	}
	if fi, err := os.Lstat(dangling); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("the dangling symlink was replaced: %v %v", fi, err)
	}
}
