package anchor

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LysCongThanh/heavyq/internal/repoconfig"
)

// mkrepo writes files (slash paths) under a fresh temp dir.
func mkrepo(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, body := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func ids(f *File) []string {
	var out []string
	for _, it := range f.Items {
		out = append(out, it.ID)
	}
	return out
}

func TestLoadMissingFilesAreFine(t *testing.T) {
	f, err := Load(t.TempDir(), repoconfig.Anchor{Extra: []string{"plans/*/phase-*.md"}})
	if err != nil {
		t.Fatal(err)
	}
	if f.Path != DefaultFile || len(f.Items) != 0 || len(f.Warnings) != 0 || f.Items == nil {
		t.Fatalf("file = %+v", f)
	}
}

func TestLoadMergesAnchorAndExtras(t *testing.T) {
	root := mkrepo(t, map[string]string{
		".heavyq/anchor.md":       "# Goal: g\n- [ ] one: first  `proof: make`\n",
		"plans/b/phase-02.md":     "- [ ] beta task\n",
		"plans/a/phase-01.md":     "- [x] alpha task\n",
		"plans/a/notes.md":        "- [ ] not matched by the glob\n",
		"docs/x/y/deep.md":        "- [ ] deep task\n",
		"docs/top.md":             "- [ ] top task\n",
		"docs/x/.git/ignored.md":  "- [ ] inside .git dir\n",
		"other/outside-glob.md":   "- [ ] outside\n",
		"plans/a/phase-01.md.bak": "- [ ] wrong suffix\n",
	})
	f, err := Load(root, repoconfig.Anchor{Extra: []string{"plans/*/phase-*.md", "docs/**/*.md", ".heavyq/*.md"}})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"one",
		"plans-a-phase-01/1-alpha-task", "plans-b-phase-02/1-beta-task", // glob 1, files sorted
		"docs-top/1-top-task", "docs-x-y-deep/1-deep-task", // glob 2 ("**" matches zero or more dirs)
		// glob 3 matches anchor.md itself: it must not be read twice
	}
	if got := ids(f); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("ids = %v\nwant  %v", got, want)
	}
	if f.Goal != "g" || f.GoalLine != 1 || f.Path != ".heavyq/anchor.md" {
		t.Fatalf("file = %+v", f)
	}
	if it := f.Items[0]; it.ReadOnly || it.Proof.Value != "make" || it.Source != ".heavyq/anchor.md" {
		t.Fatalf("anchor item = %+v", it)
	}
	if it := f.Items[1]; !it.ReadOnly || !it.Checked || it.Source != "plans/a/phase-01.md" {
		t.Fatalf("extra item = %+v", it)
	}
}

func TestLoadAnchorFileOverride(t *testing.T) {
	root := mkrepo(t, map[string]string{
		"TODO.md":           "# Goal: custom\n- [ ] x: y\n",
		".heavyq/anchor.md": "# Goal: default\n- [ ] z: w\n",
	})
	f, err := Load(root, repoconfig.Anchor{File: "./TODO.md"})
	if err != nil {
		t.Fatal(err)
	}
	if f.Path != "TODO.md" || f.Goal != "custom" || fmt.Sprint(ids(f)) != "[x]" {
		t.Fatalf("file = %+v", f)
	}
}

func TestLoadRejectsEscapingPaths(t *testing.T) {
	root := t.TempDir()
	for _, cfg := range []repoconfig.Anchor{
		{File: "../anchor.md"},
		{File: "/etc/passwd"},
		{Extra: []string{"../*.md"}},
		{Extra: []string{"/tmp/*.md"}},
		{Extra: []string{"docs/[.md"}},
	} {
		if _, err := Load(root, cfg); err == nil {
			t.Errorf("Load(%+v) succeeded", cfg)
		}
	}
}

func TestLoadSkipsSymlinksLeavingTheRepo(t *testing.T) {
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.md"), []byte("- [ ] leaked\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	root := mkrepo(t, map[string]string{"plans/a/phase-01.md": "- [ ] inside\n"})
	if err := os.Symlink(filepath.Join(outside, "secret.md"), filepath.Join(root, "plans", "a", "phase-02.md")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "plans", "linked")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "phase-03.md"), []byte("- [ ] via dir link\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := Load(root, repoconfig.Anchor{Extra: []string{"plans/*/phase-*.md", "plans/**/phase-*.md"}})
	if err != nil {
		t.Fatal(err)
	}
	if got := ids(f); fmt.Sprint(got) != "[plans-a-phase-01/1-inside]" {
		t.Fatalf("ids = %v", got)
	}
}

func TestLoadOversizedFilesAreSkippedWithWarning(t *testing.T) {
	big := "- [ ] first\n" + strings.Repeat("x", MaxFileBytes)
	root := mkrepo(t, map[string]string{
		"plans/a/phase-01.md": big,
		"plans/b/phase-01.md": "- [ ] small\n",
		".heavyq/anchor.md":   big,
	})
	f, err := Load(root, repoconfig.Anchor{Extra: []string{"plans/*/phase-*.md"}})
	if err != nil {
		t.Fatal(err)
	}
	if got := ids(f); fmt.Sprint(got) != "[plans-b-phase-01/1-small]" {
		t.Fatalf("ids = %v", got)
	}
	if len(f.Warnings) != 2 {
		t.Fatalf("warnings = %v", f.Warnings)
	}
}

func TestLoadCapsTotalItems(t *testing.T) {
	var a, b bytes.Buffer
	for i := 0; i < 1500; i++ {
		fmt.Fprintf(&a, "- [ ] a %d\n", i)
		fmt.Fprintf(&b, "- [ ] b %d\n", i)
	}
	root := mkrepo(t, map[string]string{"plans/a/phase-01.md": a.String(), "plans/b/phase-01.md": b.String()})
	f, err := Load(root, repoconfig.Anchor{Extra: []string{"plans/*/phase-*.md"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Items) != MaxItems {
		t.Fatalf("items = %d", len(f.Items))
	}
	if len(f.Warnings) == 0 || !strings.Contains(f.Warnings[len(f.Warnings)-1].Msg, "2000") {
		t.Fatalf("warnings = %v", f.Warnings)
	}
}

func TestLoadCrossFileIDCollisionGetsSuffix(t *testing.T) {
	// "a/b-c.md" and "a-b/c.md" slug to the same file prefix.
	root := mkrepo(t, map[string]string{"d/a/b-c.md": "- [ ] t\n", "d/a-b/c.md": "- [ ] t\n"})
	f, err := Load(root, repoconfig.Anchor{Extra: []string{"d/**/*.md"}})
	if err != nil {
		t.Fatal(err)
	}
	got := ids(f)
	if len(got) != 2 || got[0] == got[1] || len(f.Warnings) != 1 {
		t.Fatalf("ids = %v warnings = %v", got, f.Warnings)
	}
}

func TestMatchSegs(t *testing.T) {
	for _, tc := range []struct {
		pat, name string
		want      bool
	}{
		{"docs/**/*.md", "docs/a.md", true},
		{"docs/**/*.md", "docs/x/y/a.md", true},
		{"docs/**/*.md", "docs/x/y/a.txt", false},
		{"docs/**/*.md", "other/a.md", false},
		{"**/*.md", "a.md", true},
		{"docs/**", "docs/x/y", true},
		{"a/**/b/**/c", "a/b/c", true},
		{"a/**/b/**/c", "a/x/b/y/z/c", true},
		{"plans/*/phase-*.md", "plans/a/phase-01.md", true},
		{"plans/*/phase-*.md", "plans/a/b/phase-01.md", false},
	} {
		got := matchSegs(strings.Split(tc.pat, "/"), strings.Split(tc.name, "/"))
		if got != tc.want {
			t.Errorf("matchSegs(%q, %q) = %v", tc.pat, tc.name, got)
		}
	}
}
