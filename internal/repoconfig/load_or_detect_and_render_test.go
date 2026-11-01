package repoconfig

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

func TestLoadOrDetect(t *testing.T) {
	t.Run("no file: the detected gate", func(t *testing.T) {
		dir := newRepo(t, "")
		if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module x\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		sub := filepath.Join(dir, "pkg")
		if err := os.Mkdir(sub, 0o755); err != nil {
			t.Fatal(err)
		}
		r, err := LoadOrDetect(sub) // found from a subdirectory, at the git root
		if err != nil || r == nil || r.Root != dir || !r.Gates[0].Detected {
			t.Fatalf("LoadOrDetect = %+v, %v", r, err)
		}
		if plain, err := Load(sub); err != nil || plain != nil {
			t.Fatalf("Load must stay detection-free: %+v, %v", plain, err)
		}
	})
	t.Run("a file always wins", func(t *testing.T) {
		for name, body := range map[string]string{
			"its own gates": "[[gate]]\nname = \"mine\"\nrun = [\"true\"]\n",
			"an empty file": "# no gates\n",
		} {
			dir := newRepo(t, body)
			if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module x\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			r, err := LoadOrDetect(dir)
			if err != nil || r == nil || r.Path == "" {
				t.Fatalf("%s: LoadOrDetect = %+v, %v", name, r, err)
			}
			for _, g := range r.Gates {
				if g.Detected || g.Name != "mine" {
					t.Fatalf("%s: got gate %+v", name, g)
				}
			}
		}
	})
	t.Run("an invalid file is an error, not a detection", func(t *testing.T) {
		dir := newRepo(t, "[[gate]]\nname = \"BAD\"\nrun = [\"true\"]\n")
		if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module x\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if r, err := LoadOrDetect(dir); err == nil || r != nil {
			t.Fatalf("LoadOrDetect = %+v, %v", r, err)
		}
	})
	t.Run("nothing to detect, or no git", func(t *testing.T) {
		if r, err := LoadOrDetect(newRepo(t, "")); err != nil || r != nil {
			t.Fatalf("empty repository: %+v, %v", r, err)
		}
		plain := writeFiles(t, map[string]string{"go.mod": "module x\n"}) // not a repository
		if r, err := LoadOrDetect(plain); err != nil || r != nil {
			t.Fatalf("outside git: %+v, %v", r, err)
		}
	})
}

// What init writes must load back as exactly the gate that was detected, with
// every comment line staying a comment, for every combination of ecosystems.
func TestRenderFileRoundTrips(t *testing.T) {
	dir := writeFiles(t, map[string]string{
		"go.mod": "module x\n", "package.json": allScripts, "pnpm-lock.yaml": "", "pytest.ini": "", "Cargo.toml": ""})
	det := Detect(dir)
	text := RenderFile(det)
	repo, err := parse(dir, filepath.Join(dir, FileName), []byte(text))
	if err != nil {
		t.Fatalf("rendered file does not load: %v\n%s", err, text)
	}
	want := det.Gates[0]
	if len(repo.Gates) != 1 || repo.Gates[0].Name != want.Name || !reflect.DeepEqual(repo.Gates[0].Run, want.Run) ||
		repo.Gates[0].Prio != want.Prio || repo.Gates[0].Remote != want.Remote {
		t.Fatalf("loaded %+v, detected %+v\n%s", repo.Gates, want, text)
	}
	for _, must := range []string{"trusted like a Makefile", "heavyq gate run", "go.mod, package.json (pnpm), pytest.ini, Cargo.toml"} {
		if !strings.Contains(text, must) {
			t.Errorf("rendered file lacks %q:\n%s", must, text)
		}
	}
}

func TestRenderFileWithNothingDetectedHasNoGate(t *testing.T) {
	text := RenderFile(nil)
	repo, err := parse(t.TempDir(), "heavyq.toml", []byte(text))
	if err != nil || len(repo.Gates) != 0 {
		t.Fatalf("parse = %+v, %v\n%s", repo, err, text)
	}
	if !strings.Contains(text, "# [[gate]]") || !strings.Contains(text, "found no go.mod") {
		t.Fatalf("no example or explanation:\n%s", text)
	}
}

func TestTomlStringEscapes(t *testing.T) {
	for _, s := range []string{`plain`, `say "hi"`, `back\slash`, "tab\there", "new\nline", "bell\a", "del\x7f", "ünï"} {
		var got map[string]string
		if _, err := toml.Decode("x = "+tomlString(s)+"\n", &got); err != nil || got["x"] != s {
			t.Errorf("tomlString(%q) = %s decodes to %q, %v", s, tomlString(s), got["x"], err)
		}
	}
	if got := tomlString(`a "b" \c`); got != `"a \"b\" \\c"` {
		t.Errorf("got %s", got)
	}
}
