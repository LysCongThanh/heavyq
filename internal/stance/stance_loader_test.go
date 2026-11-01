package stance

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeStance(t *testing.T, dir, file, content string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, file)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func stanceFile(name, desc, body string) string {
	return "+++\nname = \"" + name + "\"\ndescription = \"" + desc + "\"\n+++\n" + body + "\n"
}

func TestStanceLoadBuiltinsOnly(t *testing.T) {
	all, err := Load(Dirs{})
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, s := range all {
		names = append(names, s.Name)
		if s.Source != SourceBuiltin || s.Path != "builtin:"+s.Name {
			t.Errorf("%s: source %q path %q", s.Name, s.Source, s.Path)
		}
	}
	if got := strings.Join(names, ","); got != "breakthrough,calm,converge,skeptic,surgeon" {
		t.Fatalf("names = %s", got)
	}
}

func TestStanceLoadPrecedenceRepoOverUserOverBuiltin(t *testing.T) {
	tmp := t.TempDir()
	repo, user := filepath.Join(tmp, "repo"), filepath.Join(tmp, "user")
	writeStance(t, user, "calm.md", stanceFile("calm", "user calm", "user text"))
	writeStance(t, user, "converge.md", stanceFile("converge", "user converge", "user converge text"))
	repoCalm := writeStance(t, repo, "calm.md", stanceFile("calm", "repo calm", "repo text"))
	writeStance(t, repo, "mine.md", stanceFile("mine", "repo only", "mine text"))
	writeStance(t, repo, "notes.txt", "ignored")
	writeStance(t, repo, ".hidden.md", "ignored, not even parsed")

	all, err := Load(Dirs{Repo: repo, User: user})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]struct {
		src  Source
		desc string
	}{
		"calm":         {SourceRepo, "repo calm"},
		"converge":     {SourceUser, "user converge"},
		"mine":         {SourceRepo, "repo only"},
		"breakthrough": {SourceBuiltin, ""},
		"skeptic":      {SourceBuiltin, ""},
		"surgeon":      {SourceBuiltin, ""},
	}
	if len(all) != len(want) {
		t.Fatalf("got %d stances, want %d", len(all), len(want))
	}
	for _, s := range all {
		w, ok := want[s.Name]
		if !ok || s.Source != w.src || (w.desc != "" && s.Description != w.desc) {
			t.Errorf("%s: source %q desc %q, want %+v", s.Name, s.Source, s.Description, w)
		}
	}
	calm, _ := Find(all, "calm")
	if calm.Path != repoCalm || calm.Body != "repo text" {
		t.Fatalf("calm = %+v", calm)
	}
	for i := 1; i < len(all); i++ {
		if all[i-1].Name >= all[i].Name {
			t.Fatalf("not sorted: %s before %s", all[i-1].Name, all[i].Name)
		}
	}
}

// A broken override must be an error, not a silent fall back to the stance
// it was written to replace. All broken files are reported together.
func TestStanceLoadReportsEveryBrokenFile(t *testing.T) {
	tmp := t.TempDir()
	repo, user := filepath.Join(tmp, "repo"), filepath.Join(tmp, "user")
	bad1 := writeStance(t, repo, "calm.md", "+++\nname = \"calm\"\nbogus = 1\n+++\nx\n")
	bad2 := writeStance(t, user, "skeptic.md", "no frontmatter")
	_, err := Load(Dirs{Repo: repo, User: user})
	if err == nil {
		t.Fatal("want an error")
	}
	for _, want := range []string{bad1 + ":3: unknown key", bad2 + ":1: file must start"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q lacks %q", err, want)
		}
	}
}

func TestStanceLoadRejectsOversizeAndNonRegularFiles(t *testing.T) {
	tmp := t.TempDir()
	writeStance(t, tmp, "big.md", "+++\n+++\n"+strings.Repeat("a", MaxBodyBytes+maxFrontmatterBytes+100))
	if err := os.Mkdir(filepath.Join(tmp, "dir.md"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/dev/zero", filepath.Join(tmp, "zero.md")); err != nil {
		t.Fatal(err)
	}
	_, err := Load(Dirs{Repo: tmp})
	if err == nil || !strings.Contains(err.Error(), "big.md") || !strings.Contains(err.Error(), "larger than") || !strings.Contains(err.Error(), "zero.md: not a regular file") {
		t.Fatalf("err = %v", err)
	}
}

func TestStanceDefaultDirsUsesXDGConfigHome(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	if got, want := DefaultDirs(tmp).User, filepath.Join(tmp, "heavyq", "stances"); got != want {
		t.Fatalf("user dir = %q, want %q", got, want)
	}
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", tmp)
	if got, want := DefaultDirs(tmp).User, filepath.Join(tmp, ".config", "heavyq", "stances"); got != want {
		t.Fatalf("fallback user dir = %q, want %q", got, want)
	}
	t.Setenv("XDG_CONFIG_HOME", "relative/path") // not absolute: ignored, per the XDG spec
	if got, want := DefaultDirs(tmp).User, filepath.Join(tmp, ".config", "heavyq", "stances"); got != want {
		t.Fatalf("relative XDG dir = %q, want %q", got, want)
	}
}
