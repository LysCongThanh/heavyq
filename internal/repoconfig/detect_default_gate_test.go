package repoconfig

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// writeFiles creates the files (name -> content) in a fresh temp dir.
func writeFiles(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

const allScripts = `{"scripts": {"typecheck": "tsc --noEmit", "lint": "eslint .", "test": "vitest run", "build": "tsc"}}`

func TestDetectTable(t *testing.T) {
	cases := []struct {
		name    string
		files   map[string]string
		want    []string // nil: nothing detected
		sources []string
	}{
		{"empty directory", map[string]string{}, nil, nil},
		{"unrelated files only", map[string]string{"Makefile": "test:\n", "README.md": "x"}, nil, nil},
		{"go", map[string]string{"go.mod": "module x\n"},
			[]string{"go vet ./...", "go test ./..."}, []string{"go.mod"}},
		{"cargo", map[string]string{"Cargo.toml": "[package]\n"},
			[]string{"cargo test"}, []string{"Cargo.toml"}},
		{"pyproject", map[string]string{"pyproject.toml": "[project]\n"},
			[]string{"pytest"}, []string{"pyproject.toml"}},
		{"pytest.ini", map[string]string{"pytest.ini": "[pytest]\n"},
			[]string{"pytest"}, []string{"pytest.ini"}},
		{"setup.cfg with a pytest section", map[string]string{"setup.cfg": "[metadata]\nname = x\n\n  [tool:pytest]\naddopts = -q\n"},
			[]string{"pytest"}, []string{"setup.cfg"}},
		{"setup.cfg without one", map[string]string{"setup.cfg": "[metadata]\nname = x\n# [tool:pytest]\n"}, nil, nil},
		{"two python markers count once", map[string]string{"pyproject.toml": "", "pytest.ini": ""},
			[]string{"pytest"}, []string{"pytest.ini"}},
		{"npm, no lockfile", map[string]string{"package.json": allScripts},
			[]string{"npm run typecheck", "npm run lint", "npm run test"}, []string{"package.json (npm)"}},
		{"npm with a lockfile", map[string]string{"package.json": allScripts, "package-lock.json": "{}"},
			[]string{"npm run typecheck", "npm run lint", "npm run test"}, []string{"package.json (npm)"}},
		{"pnpm", map[string]string{"package.json": allScripts, "pnpm-lock.yaml": ""},
			[]string{"pnpm run typecheck", "pnpm run lint", "pnpm run test"}, []string{"package.json (pnpm)"}},
		{"yarn", map[string]string{"package.json": allScripts, "yarn.lock": ""},
			[]string{"yarn run typecheck", "yarn run lint", "yarn run test"}, []string{"package.json (yarn)"}},
		{"bun.lockb", map[string]string{"package.json": allScripts, "bun.lockb": ""},
			[]string{"bun run typecheck", "bun run lint", "bun run test"}, []string{"package.json (bun)"}},
		{"bun.lock", map[string]string{"package.json": allScripts, "bun.lock": ""},
			[]string{"bun run typecheck", "bun run lint", "bun run test"}, []string{"package.json (bun)"}},
		{"several lockfiles: pnpm first", map[string]string{"package.json": allScripts, "yarn.lock": "", "pnpm-lock.yaml": ""},
			[]string{"pnpm run typecheck", "pnpm run lint", "pnpm run test"}, []string{"package.json (pnpm)"}},
		{"only the scripts that exist", map[string]string{"package.json": `{"scripts": {"test": "jest", "start": "node ."}}`},
			[]string{"npm run test"}, []string{"package.json (npm)"}},
		{"lint and typecheck, no test", map[string]string{"package.json": `{"scripts": {"lint": "eslint .", "typecheck": "tsc"}}`},
			[]string{"npm run typecheck", "npm run lint"}, []string{"package.json (npm)"}},
		{"package.json without scripts", map[string]string{"package.json": `{"name": "x"}`}, nil, nil},
		{"package.json with unrelated scripts", map[string]string{"package.json": `{"scripts": {"build": "tsc", "start": "node ."}}`}, nil, nil},
		{"the npm init test stub is no test", map[string]string{"package.json": `{"scripts": {"test": "echo \"Error: no test specified\" && exit 1"}}`}, nil, nil},
		{"blank and non-string scripts are skipped", map[string]string{"package.json": `{"scripts": {"typecheck": "  ", "lint": ["eslint"], "test": 7}}`}, nil, nil},
		{"malformed package.json", map[string]string{"package.json": `{"scripts": {"test": "jest"`}, nil, nil},
		{"package.json that is not an object", map[string]string{"package.json": `["test"]`}, nil, nil},
		{"scripts that is not an object", map[string]string{"package.json": `{"scripts": "test"}`}, nil, nil},
		{"malformed package.json does not hide go", map[string]string{"package.json": "{", "go.mod": "module x\n"},
			[]string{"go vet ./...", "go test ./..."}, []string{"go.mod"}},
		{"go and node", map[string]string{"go.mod": "module x\n", "package.json": `{"scripts": {"test": "jest"}}`},
			[]string{"go vet ./...", "go test ./...", "npm run test"}, []string{"go.mod", "package.json (npm)"}},
		{"every ecosystem, in order", map[string]string{
			"Cargo.toml": "", "pyproject.toml": "", "package.json": allScripts, "yarn.lock": "", "go.mod": "module x\n"},
			[]string{"go vet ./...", "go test ./...", "yarn run typecheck", "yarn run lint", "yarn run test", "pytest", "cargo test"},
			[]string{"go.mod", "package.json (yarn)", "pyproject.toml", "Cargo.toml"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := writeFiles(t, c.files)
			r := Detect(dir)
			if c.want == nil {
				if r != nil {
					t.Fatalf("detected %+v, want nothing", r.Gates)
				}
				return
			}
			if r == nil || len(r.Gates) != 1 {
				t.Fatalf("Detect = %+v, want one gate", r)
			}
			g := r.Gates[0]
			if g.Name != "ready" || !g.Detected || g.Prio != DefaultPrio || g.Remote != DefaultRemote || g.Light {
				t.Errorf("gate = %+v", g)
			}
			if !reflect.DeepEqual(g.Run, c.want) {
				t.Errorf("run = %q, want %q", g.Run, c.want)
			}
			if !reflect.DeepEqual(g.Sources, c.sources) {
				t.Errorf("sources = %q, want %q", g.Sources, c.sources)
			}
			if r.Root != dir || r.Path != "" || !strings.Contains(r.Where(), dir) {
				t.Errorf("root/path/where = %q %q %q", r.Root, r.Path, r.Where())
			}
		})
	}
}

// A directory named like a marker file is not the file.
func TestDetectIgnoresDirectoriesNamedLikeMarkers(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"go.mod", "Cargo.toml", "pyproject.toml", "package.json", "setup.cfg"} {
		if err := os.Mkdir(filepath.Join(dir, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if r := Detect(dir); r != nil {
		t.Fatalf("detected %+v", r.Gates)
	}
}

// A package.json over the size bound is ignored, not read, and does not make
// detection fail or stall.
func TestDetectIgnoresAHugePackageJSON(t *testing.T) {
	huge := `{"scripts": {"test": "jest"}, "pad": "` + strings.Repeat("x", maxFileBytes) + `"}`
	dir := writeFiles(t, map[string]string{"package.json": huge, "Cargo.toml": ""})
	r := Detect(dir)
	if r == nil || !reflect.DeepEqual(r.Gates[0].Run, []string{"cargo test"}) {
		t.Fatalf("Detect = %+v, want only cargo test", r)
	}
	// ... and a huge setup.cfg likewise.
	dir = writeFiles(t, map[string]string{"setup.cfg": strings.Repeat("a = b\n", maxFileBytes/4) + "[tool:pytest]\n"})
	if r := Detect(dir); r != nil {
		t.Fatalf("detected %+v from a setup.cfg over the bound", r.Gates)
	}
}

// Detection reads files. Even when every script body is a command that would
// leave a trace, nothing runs, and no script body reaches a gate command.
func TestDetectNeverRunsAnything(t *testing.T) {
	dir := t.TempDir()
	trace := filepath.Join(dir, "pwned")
	scripts := `{"scripts": {"typecheck": "touch ` + trace + `", "lint": "touch ` + trace + `", "test": "touch ` + trace + `"},` +
		`"packageManager": "npm@1; touch ` + trace + `"}`
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(scripts), 0o600); err != nil {
		t.Fatal(err)
	}
	r := Detect(dir)
	if r == nil {
		t.Fatal("nothing detected")
	}
	for _, cmd := range r.Gates[0].Run {
		if strings.Contains(cmd, "touch") || strings.Contains(cmd, dir) {
			t.Errorf("script body reached the command %q", cmd)
		}
	}
	if _, err := os.Stat(trace); err == nil {
		t.Fatal("detection ran a script")
	}
}
