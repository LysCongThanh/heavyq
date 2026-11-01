package repoconfig

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// newRepo makes a real git repository in a temp dir and, when body is not
// empty, writes heavyq.toml into it. The returned path is symlink-resolved so
// it compares equal to what `git rev-parse --show-toplevel` prints.
func newRepo(t *testing.T, body string) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "-C", dir, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	if body != "" {
		if err := os.WriteFile(filepath.Join(dir, FileName), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestLoadFullFile(t *testing.T) {
	dir := newRepo(t, `
[[gate]]
name = "ready"
run = ["gofmt -l . | (! grep .)", "go vet ./...", "go test -race ./..."]
prio = 2
light = true
remote = "auto"

[[gate]]
name = "smoke_1-b"
run = ["make smoke"]

[anchor]
file = ".heavyq/anchor.md"
extra = ["plans/*/phase-*.md", "docs/**/*.md"]
`)
	r, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if r == nil || r.Root != dir || r.Path != filepath.Join(dir, FileName) {
		t.Fatalf("repo = %+v", r)
	}
	want := []Gate{
		{Name: "ready", Run: []string{"gofmt -l . | (! grep .)", "go vet ./...", "go test -race ./..."},
			Prio: 2, Light: true, Remote: "auto"},
		{Name: "smoke_1-b", Run: []string{"make smoke"}, Prio: DefaultPrio, Remote: DefaultRemote},
	}
	if !reflect.DeepEqual(r.Gates, want) {
		t.Fatalf("gates = %+v\nwant   %+v", r.Gates, want)
	}
	if r.Anchor.File != ".heavyq/anchor.md" || len(r.Anchor.Extra) != 2 {
		t.Fatalf("anchor = %+v", r.Anchor)
	}
	if g, ok := r.Gate("smoke_1-b"); !ok || g.Prio != 3 {
		t.Fatalf("Gate lookup = %+v %v", g, ok)
	}
	if _, ok := r.Gate("nope"); ok {
		t.Fatal("unknown gate found")
	}
}

func TestLoadFindsRootFromSubdirectory(t *testing.T) {
	dir := newRepo(t, "[[gate]]\nname = \"ready\"\nrun = [\"true\"]\n")
	sub := filepath.Join(dir, "pkg", "deep")
	if err := os.MkdirAll(sub, 0o700); err != nil {
		t.Fatal(err)
	}
	r, err := Load(sub)
	if err != nil || r == nil || r.Root != dir || len(r.Gates) != 1 {
		t.Fatalf("Load(subdir) = %+v, %v", r, err)
	}
}

func TestLoadAbsentIsNotAnError(t *testing.T) {
	notRepo := t.TempDir()
	if r, err := Load(notRepo); r != nil || err != nil {
		t.Fatalf("outside a repo: %+v, %v", r, err)
	}
	if r, err := Load(filepath.Join(notRepo, "does-not-exist")); r != nil || err != nil {
		t.Fatalf("missing dir: %+v, %v", r, err)
	}
	if r, err := Load(newRepo(t, "")); r != nil || err != nil {
		t.Fatalf("repo without heavyq.toml: %+v, %v", r, err)
	}
}

func TestLoadEmptyFileHasNoGates(t *testing.T) {
	r, err := Load(newRepo(t, "# nothing yet\n"))
	if err != nil || r == nil || len(r.Gates) != 0 {
		t.Fatalf("Load = %+v, %v", r, err)
	}
}

func TestLoadRejectsInvalidFiles(t *testing.T) {
	long := strings.Repeat("x", MaxRunLen+1)
	cases := []struct{ name, body, want string }{
		{"bad syntax", "[[gate]\n", "heavyq.toml"},
		{"name upper case", "[[gate]]\nname = \"Ready\"\nrun = [\"true\"]\n", `name "Ready" must match`},
		{"name starts with dash", "[[gate]]\nname = \"-x\"\nrun = [\"true\"]\n", "must match"},
		{"name too long", "[[gate]]\nname = \"" + strings.Repeat("a", 41) + "\"\nrun = [\"true\"]\n", "must match"},
		{"name missing", "[[gate]]\nrun = [\"true\"]\n", "[[gate]] #1: name"},
		{"run missing", "[[gate]]\nname = \"a\"\n", "run must be a non-empty list"},
		{"run empty list", "[[gate]]\nname = \"a\"\nrun = []\n", "run must be a non-empty list"},
		{"run blank string", "[[gate]]\nname = \"a\"\nrun = [\"go vet\", \"  \"]\n", "run[1] is empty"},
		{"run too long", "[[gate]]\nname = \"a\"\nrun = [\"" + long + "\"]\n", "run[0] is 2001 bytes"},
		{"run is a string", "[[gate]]\nname = \"a\"\nrun = \"go test\"\n", "heavyq.toml"},
		{"prio zero", "[[gate]]\nname = \"a\"\nrun = [\"x\"]\nprio = 0\n", "prio 0 must be between 1 and 9"},
		{"prio ten", "[[gate]]\nname = \"a\"\nrun = [\"x\"]\nprio = 10\n", "prio 10 must be between 1 and 9"},
		{"prio string", "[[gate]]\nname = \"a\"\nrun = [\"x\"]\nprio = \"high\"\n", "heavyq.toml"},
		{"remote unsafe", "[[gate]]\nname = \"a\"\nrun = [\"x\"]\nremote = \"a b; rm\"\n", `remote "a b; rm" must be`},
		{"duplicate", "[[gate]]\nname = \"a\"\nrun = [\"x\"]\n[[gate]]\nname = \"a\"\nrun = [\"y\"]\n", `[[gate]] #2: duplicate gate name "a"`},
		{"anchor file escapes", "[anchor]\nfile = \"../x.md\"\n", `file "../x.md" must be a relative path`},
		{"anchor extra absolute", "[anchor]\nextra = [\"/etc/*\"]\n", "extra[0]"},
		{"anchor extra bad glob", "[anchor]\nextra = [\"[a\"]\n", "extra[0]"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r, err := Load(newRepo(t, c.body))
			if err == nil || r != nil {
				t.Fatalf("want error, got %+v, %v", r, err)
			}
			if !strings.Contains(err.Error(), c.want) || !strings.Contains(err.Error(), FileName) {
				t.Fatalf("error %q: want %q and the file name", err, c.want)
			}
		})
	}
}

func TestLoadUnknownKeysReportFileAndLine(t *testing.T) {
	cases := []struct{ name, body, want string }{
		{"typo in gate", "[[gate]]\nname = \"a\"\nrun = [\"x\"]\n\n[[gate]]\nname = \"b\"\nrun = [\"y\"]\nlight_job = true\n", `heavyq.toml:8: unknown key "gate.light_job"`},
		{"unknown table", "[[gate]]\nname = \"a\"\nrun = [\"x\"]\n\n[stop]\ndone = true\n", `heavyq.toml:5: unknown key "stop"`},
		{"top level key", "version = 1\n[[gate]]\nname = \"a\"\nrun = [\"x\"]\n", `heavyq.toml:1: unknown key "version"`},
		{"anchor key", "# c\n[anchor]\nfile = \"a.md\"\nfiles = \"b\"\n", `heavyq.toml:4: unknown key "anchor.files"`},
		{"gates plural", "[[gates]]\nname = \"a\"\n", `heavyq.toml:1: unknown key "gates"`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := Load(newRepo(t, c.body))
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("error = %v, want it to contain %q", err, c.want)
			}
		})
	}
}

func TestLoadRefusesOversizedAndNonRegularFiles(t *testing.T) {
	big := newRepo(t, "# "+strings.Repeat("x", maxFileBytes)+"\n")
	if _, err := Load(big); err == nil || !strings.Contains(err.Error(), "larger than") {
		t.Fatalf("oversized: %v", err)
	}
	dir := newRepo(t, "")
	if err := os.Mkdir(filepath.Join(dir, FileName), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(dir); err == nil || !strings.Contains(err.Error(), "not a regular file") {
		t.Fatalf("directory named heavyq.toml: %v", err)
	}
}

func TestLoadFollowsSymlinkToRegularFile(t *testing.T) {
	dir := newRepo(t, "")
	target := filepath.Join(dir, "gates.toml")
	if err := os.WriteFile(target, []byte("[[gate]]\nname = \"a\"\nrun = [\"x\"]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("gates.toml", filepath.Join(dir, FileName)); err != nil {
		t.Fatal(err)
	}
	if r, err := Load(dir); err != nil || r == nil || len(r.Gates) != 1 {
		t.Fatalf("Load = %+v, %v", r, err)
	}
}
