package stance

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestStanceDirIgnoresNonStanceFiles: a README, notes or a draft in a stance
// directory must not break the loader (#51). They are ignored and reported as
// warnings; a file that looks like a stance and is broken still fails loudly.
func TestStanceDirIgnoresNonStanceFiles(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "stances")
	writeStance(t, dir, "mine.md", stanceFile("mine", "a real one", "mine text"))
	writeStance(t, dir, "README.md", "# Stances\n\nHow we use them.\n")        // invalid name
	writeStance(t, dir, "Draft Idea.md", "+++\ndescription = \"x\"\n+++\nx\n") // invalid name, even with a fence
	writeStance(t, dir, "notes.md", "just some notes, no frontmatter\n")       // valid name, no fence
	writeStance(t, dir, "empty.md", "")                                        // valid name, empty
	writeStance(t, dir, "late-fence.md", "intro\n+++\ndescription = \"x\"\n+++\nbody\n")
	writeStance(t, dir, "todo.txt", "not markdown at all")
	writeStance(t, dir, ".hidden.md", "ignored silently")
	if err := os.MkdirAll(filepath.Join(dir, "sub.md"), 0o755); err != nil {
		t.Fatal(err)
	}

	all, warns, err := LoadReport(Dirs{Repo: dir})
	if err != nil {
		t.Fatalf("non-stance files must not break the loader: %v", err)
	}
	if s, ok := Find(all, "mine"); !ok || s.Source != SourceRepo || s.Body != "mine text" {
		t.Fatalf("the real stance must load: %+v", s)
	}
	if len(all) != 6 { // five built-ins and "mine"
		t.Fatalf("loaded %d stances, want 6", len(all))
	}
	got := map[string]string{}
	for _, w := range warns {
		got[filepath.Base(w.Path)] = w.Reason
	}
	want := map[string]string{
		"README.md":     "not a valid stance name",
		"Draft Idea.md": "not a valid stance name",
		"notes.md":      "+++ frontmatter",
		"empty.md":      "+++ frontmatter",
		"late-fence.md": "+++ frontmatter",
	}
	if len(got) != len(want) || len(warns) != len(want) {
		t.Fatalf("warnings = %v, want exactly %d files", warns, len(want))
	}
	for file, frag := range want {
		if !strings.Contains(got[file], frag) {
			t.Errorf("warning for %s = %q, want it to mention %q", file, got[file], frag)
		}
	}
	if again, err := Load(Dirs{Repo: dir}); err != nil || len(again) != 6 {
		t.Fatalf("Load must ignore them too: %d %v", len(again), err)
	}
}

func TestStanceDirBrokenStanceStillFailsLoudly(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "stances")
	writeStance(t, dir, "README.md", "# readme\n")
	bad := writeStance(t, dir, "broken.md", "+++\ncooldown_minute = 5\n+++\nbody\n") // typo'd key
	badName := writeStance(t, dir, "open.md", "+++\ndescription = \"never closed\"\nbody\n")
	mismatch := writeStance(t, dir, "other.md", "+++\nname = \"different\"\n+++\nbody\n")

	_, _, err := LoadReport(Dirs{Repo: dir})
	if err == nil {
		t.Fatal("broken stance files must fail the load")
	}
	for _, want := range []string{bad + ":2: unknown key", badName, mismatch} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error lacks %q:\n%v", want, err)
		}
	}
	if strings.Contains(err.Error(), "README") {
		t.Errorf("the README must not be part of the error:\n%v", err)
	}
}

func TestStanceDirAcceptsBOMAndCRLFFence(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "stances")
	writeStance(t, dir, "bom.md", "\xef\xbb\xbf+++\r\ndescription = \"bom\"\r\n+++\r\nbody\r\n")
	all, warns, err := LoadReport(Dirs{User: dir})
	if err != nil || len(warns) != 0 {
		t.Fatalf("err=%v warns=%v", err, warns)
	}
	if _, ok := Find(all, "bom"); !ok {
		t.Fatal("a BOM and CRLF stance must load")
	}
}

// A README in the repo's stance directory is no part of what the user trusts:
// editing it must not revoke the trust, and it adds no name.
func TestStanceTrustHashLeavesOutNonStanceFiles(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "stances")
	writeStance(t, dir, "mine.md", stanceFile("mine", "d", "text"))
	h1, names, err := ContentHash(dir)
	if err != nil || h1 == "" {
		t.Fatalf("hash: %q %v", h1, err)
	}
	writeStance(t, dir, "README.md", "# one\n")
	writeStance(t, dir, "notes.md", "notes\n")
	writeStance(t, dir, "README.md", "# two, edited\n")
	h2, names2, err := ContentHash(dir)
	if err != nil || h2 != h1 || len(names2) != len(names) {
		t.Fatalf("ignored files changed the hash: %q vs %q (%v)", h1, h2, names2)
	}
}
