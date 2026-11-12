package repoconfig

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The anchor file is left out of every tree identity, so heavyq.toml must not
// be able to name a directory or a code file there: that would take code out
// of what a proof and the cache cover.
func TestLoadRejectsAnAnchorFileThatIsNotOneMarkdownFile(t *testing.T) {
	cases := []struct{ name, file, want string }{
		{"code directory", "src", "must name a .md file"},
		{"trailing slash", "plans.md/", "must name a .md file, not a directory"},
		{"the root", ".", "must name a .md file"},
		{"code file", "main.go", "must name a .md file"},
		{"control character", "a\tb.md", "control character"},
		{"directory named like markdown", "notes.md", "is a directory"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := newRepo(t, "[anchor]\nfile = \""+strings.ReplaceAll(c.file, "\t", `\t`)+"\"\n")
			for _, d := range []string{"src", "notes.md"} {
				if err := os.MkdirAll(filepath.Join(dir, d), 0o700); err != nil {
					t.Fatal(err)
				}
			}
			r, err := Load(dir)
			if err == nil || r != nil {
				t.Fatalf("want error, got %+v, %v", r, err)
			}
			if !strings.Contains(err.Error(), c.want) || !strings.Contains(err.Error(), "[anchor]") {
				t.Fatalf("error %q: want %q", err, c.want)
			}
		})
	}
}

func TestLoadAcceptsAnchorMarkdownFiles(t *testing.T) {
	for _, file := range []string{".heavyq/anchor.md", "docs/./TODO.md", "PLAN.MD", "plans/not-yet-written.md"} {
		dir := newRepo(t, "[anchor]\nfile = \""+file+"\"\n")
		if err := os.MkdirAll(filepath.Join(dir, "docs"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "docs", "TODO.md"), []byte("- [ ] x\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if r, err := Load(dir); err != nil || r == nil || r.Anchor.File != file {
			t.Fatalf("%s: Load = %+v, %v", file, r, err)
		}
	}
}

func TestCheckAnchorFile(t *testing.T) {
	for p, ok := range map[string]bool{
		".heavyq/anchor.md": true, "a.md": true, "x/../a.md": true, "A.Md": true,
		"": false, ".": false, "..": false, "../a.md": false, "/a.md": false, "a.md/": false,
		"src": false, "a.go": false, ".md.txt": false, "a\x00.md": false, "a\n.md": false,
	} {
		if err := CheckAnchorFile(p); (err == nil) != ok {
			t.Errorf("CheckAnchorFile(%q) = %v, want ok=%v", p, err, ok)
		}
	}
}
