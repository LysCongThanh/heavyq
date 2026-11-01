package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const initBegin, initEnd = "<!-- heavyq:begin -->", "<!-- heavyq:end -->"

func mustRead(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestE2EInitWritesRefusesAndForces(t *testing.T) {
	h := newRig(t, baseConfig)
	repo := goModRepo(t, h)
	sub := filepath.Join(repo, "deep")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	h.cwd = sub // init writes at the git root, wherever it is started
	path := filepath.Join(repo, "heavyq.toml")

	rc, out, errOut := h.runSplit("init")
	if rc != 0 || !strings.Contains(out, "wrote "+path) || !strings.Contains(out, "go vet ./..., go test ./...") || errOut != "" {
		t.Fatalf("init rc=%d\nstdout: %s\nstderr: %s", rc, out, errOut)
	}
	text := mustRead(t, path)
	for _, want := range []string{`name = "ready"`, `"go vet ./...",`, `"go test ./...",`, "trusted like a Makefile", "To add a gate"} {
		if !strings.Contains(text, want) {
			t.Errorf("heavyq.toml lacks %q:\n%s", want, text)
		}
	}
	if _, err := os.Stat(filepath.Join(sub, "heavyq.toml")); err == nil {
		t.Fatal("heavyq.toml written in the subdirectory, not the git root")
	}
	// The file is valid: gate status reads it, and it no longer says "detected".
	if rc, out, _ := h.runSplit("gate", "status"); rc != 1 || !strings.Contains(out, "gate ready: NOT PROVEN") {
		t.Fatalf("gate status on the written file rc=%d\n%s", rc, out)
	}

	// A second run refuses and leaves the file alone, even if it was edited.
	mine := text + "# my note\n"
	if err := os.WriteFile(path, []byte(mine), 0o644); err != nil {
		t.Fatal(err)
	}
	rc, out, errOut = h.runSplit("init")
	if rc != 1 || !strings.Contains(errOut, "already exists") || !strings.Contains(errOut, "--force") || out != "" {
		t.Fatalf("second init rc=%d\nstdout: %s\nstderr: %s", rc, out, errOut)
	}
	if got := mustRead(t, path); got != mine {
		t.Fatalf("a refused init changed the file:\n%s", got)
	}

	// --print never writes and never refuses; it prints exactly what init writes.
	rc, printed, _ := h.runSplit("init", "--print")
	if rc != 0 || printed != text || mustRead(t, path) != mine {
		t.Fatalf("--print rc=%d, changed=%v\n%s", rc, mustRead(t, path) != mine, printed)
	}

	// --force overwrites.
	if rc, out, errOut = h.runSplit("init", "--force"); rc != 0 || !strings.Contains(out, "wrote ") {
		t.Fatalf("init --force rc=%d\nstdout: %s\nstderr: %s", rc, out, errOut)
	}
	if got := mustRead(t, path); got != text {
		t.Fatalf("--force did not restore the generated file:\n%s", got)
	}
	// A symlink is replaced, never written through.
	elsewhere := filepath.Join(h.root, "elsewhere.toml")
	if err := os.WriteFile(elsewhere, []byte("untouched\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(elsewhere, path); err != nil {
		t.Fatal(err)
	}
	if rc, _, _ := h.runSplit("init"); rc != 1 {
		t.Fatalf("init over a symlink must refuse, rc=%d", rc)
	}
	if rc, _, _ := h.runSplit("init", "--force"); rc != 0 || mustRead(t, elsewhere) != "untouched\n" || mustRead(t, path) != text {
		t.Fatalf("--force must replace the symlink itself, rc=%d", rc)
	}
}

func TestE2EInitUsageAndNothingDetected(t *testing.T) {
	h := newRig(t, baseConfig)
	if rc, _, errOut := h.runSplit("init"); rc != ExitUsage || !strings.Contains(errOut, "not inside a git work tree") {
		t.Fatalf("outside git: rc=%d\n%s", rc, errOut)
	}
	repo := gitRepo(t, h) // marker.txt only: nothing to detect
	for _, args := range [][]string{{"init", "extra"}, {"init", "--bogus"}, {"init", "--print", "--agents-md"}} {
		if rc, _, _ := h.runSplit(args...); rc != ExitUsage {
			t.Errorf("heavyq %v: rc=%d, want %d", args, rc, ExitUsage)
		}
	}
	rc, _, errOut := h.runSplit("init")
	if rc != 0 || !strings.Contains(errOut, "found no go.mod") {
		t.Fatalf("nothing detected: rc=%d\n%s", rc, errOut)
	}
	text := mustRead(t, filepath.Join(repo, "heavyq.toml"))
	if !strings.Contains(text, "# [[gate]]") || strings.Contains(text, "\n[[gate]]") {
		t.Fatalf("no gate may be written when nothing is detected:\n%s", text)
	}
	if rc, _, errOut := h.runSplit("gate", "status"); rc != ExitUsage || !strings.Contains(errOut, "defines no gates") {
		t.Fatalf("an example-only file defines no gate, so gate status exits 2: rc=%d\n%s", rc, errOut)
	}
}

func TestE2EInitAgentsMDIsIdempotent(t *testing.T) {
	h := newRig(t, baseConfig)
	repo := goModRepo(t, h)
	agents := filepath.Join(repo, "AGENTS.md")
	claude := filepath.Join(repo, "CLAUDE.md")

	// No AGENTS.md: it is created; no CLAUDE.md: none is created.
	rc, out, errOut := h.runSplit("init", "--agents-md")
	if rc != 0 || !strings.Contains(out, "AGENTS.md: created") || errOut != "" {
		t.Fatalf("rc=%d\nstdout: %s\nstderr: %s", rc, out, errOut)
	}
	if _, err := os.Stat(claude); err == nil {
		t.Fatal("CLAUDE.md must not be created")
	}
	created := mustRead(t, agents)
	for _, want := range []string{"Before saying a task is done, run `heavyq gate run ready`", "Exit 1:", "Exit 75:", "never bypass heavyq"} {
		if !strings.Contains(created, want) {
			t.Errorf("AGENTS.md lacks %q:\n%s", want, created)
		}
	}

	// Running it again: heavyq.toml is refused (exit 1) but the section is
	// applied and unchanged, so there is still exactly one block.
	rc, out, errOut = h.runSplit("init", "--agents-md")
	if rc != 1 || !strings.Contains(errOut, "already exists") || !strings.Contains(out, "AGENTS.md: already up to date") {
		t.Fatalf("second run rc=%d\nstdout: %s\nstderr: %s", rc, out, errOut)
	}
	if got := mustRead(t, agents); got != created {
		t.Fatalf("second run changed AGENTS.md:\n%s", got)
	}
	// ... and with --force too: still one block, and exit 0.
	if rc, _, errOut = h.runSplit("init", "--agents-md", "--force"); rc != 0 {
		t.Fatalf("--force rc=%d\n%s", rc, errOut)
	}
	if got := mustRead(t, agents); got != created || strings.Count(got, initBegin) != 1 || strings.Count(got, initEnd) != 1 {
		t.Fatalf("--force changed AGENTS.md:\n%s", got)
	}

	// An existing AGENTS.md keeps its text; CLAUDE.md, present, gets the section too.
	if err := os.WriteFile(agents, []byte("# Rules\n\nBe kind.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(claude, []byte("# Claude\nNo trailing newline"), 0o600); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if rc, _, errOut = h.runSplit("init", "--agents-md", "--force"); rc != 0 {
			t.Fatalf("rc=%d\n%s", rc, errOut)
		}
	}
	for file, prefix := range map[string]string{agents: "# Rules\n\nBe kind.\n\n" + initBegin, claude: "# Claude\nNo trailing newline\n\n" + initBegin} {
		got := mustRead(t, file)
		if !strings.HasPrefix(got, prefix) || strings.Count(got, initBegin) != 1 || strings.Count(got, initEnd) != 1 || !strings.HasSuffix(got, initEnd+"\n") {
			t.Errorf("%s:\n%s", file, got)
		}
	}
	if fi, err := os.Stat(claude); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("the file mode must be kept: %v %v", fi, err)
	}
}

// With a hand-written heavyq.toml, --agents-md still adds the section and the
// toml is left alone. A symlink out of the repository is never followed.
func TestE2EInitAgentsMDKeepsTomlAndRefusesEscapingSymlink(t *testing.T) {
	h := newRig(t, baseConfig)
	repo := goModRepo(t, h)
	toml := "[[gate]]\nname = \"ready\"\nrun = [\"true\"]\n"
	if err := os.WriteFile(filepath.Join(repo, "heavyq.toml"), []byte(toml), 0o644); err != nil {
		t.Fatal(err)
	}
	if rc, _, _ := h.runSplit("init", "--agents-md"); rc != 1 || mustRead(t, filepath.Join(repo, "heavyq.toml")) != toml ||
		!strings.Contains(mustRead(t, filepath.Join(repo, "AGENTS.md")), initBegin) {
		t.Fatalf("hand-written heavyq.toml must survive and AGENTS.md must be written, rc=%d", rc)
	}

	outside := filepath.Join(h.root, "outside.md")
	if err := os.WriteFile(outside, []byte("keep\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(repo, "AGENTS.md")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(repo, "AGENTS.md")); err != nil {
		t.Fatal(err)
	}
	rc, _, errOut := h.runSplit("init", "--agents-md", "--force")
	if rc != 1 || !strings.Contains(errOut, "outside the repository") || mustRead(t, outside) != "keep\n" {
		t.Fatalf("rc=%d, outside=%q\n%s", rc, mustRead(t, outside), errOut)
	}
}
