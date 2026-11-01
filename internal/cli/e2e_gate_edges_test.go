package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A submodule with edits of its own gives the work tree no reliable identity:
// status says so and proves nothing, and run refuses instead of running
// commands whose results could never count.
func TestE2EGateUnknownTreeSaysWhy(t *testing.T) {
	h := newRig(t, baseConfig)
	repo := gateRepo(t, h, "")
	src := filepath.Join(h.root, "lib")
	for _, args := range [][]string{{"init", "-q", "-b", "main"}, {"config", "commit.gpgsign", "false"}} {
		if err := os.MkdirAll(src, 0o755); err != nil {
			t.Fatal(err)
		}
		gitIn(t, h, src, args...)
	}
	if err := os.WriteFile(filepath.Join(src, "f.txt"), []byte("1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, h, src, "add", ".")
	gitIn(t, h, src, "commit", "-q", "-m", "lib")
	gitIn(t, h, repo, "-c", "protocol.file.allow=always", "submodule", "add", "-q", src, "mod")
	gitIn(t, h, repo, "commit", "-q", "-m", "add submodule")

	if rc, g := h.gateJSON(); rc != 1 || g.Tree == "" {
		t.Fatalf("clean submodule: rc=%d %+v", rc, g)
	}
	if err := os.WriteFile(filepath.Join(repo, "mod", "f.txt"), []byte("edited in the submodule\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rc, g := h.gateJSON()
	if rc != 1 || g.Proven || g.Tree != "" || !strings.Contains(g.Reason, "submodule") || g.Commands[0].State != "missing" {
		t.Fatalf("status: rc=%d %+v", rc, g)
	}
	rc, out := h.run("gate", "status")
	if rc != 1 || !strings.Contains(out, "an unidentified tree") || !strings.Contains(out, "nothing can be proven") {
		t.Fatalf("status text rc=%d\n%s", rc, out)
	}
	rc, out = h.run("gate", "run")
	if rc != 1 || h.counts() != "000" || !strings.Contains(out, "cannot be proven") {
		t.Fatalf("run must refuse: rc=%d counts=%s\n%s", rc, h.counts(), out)
	}
}

func TestE2EGateUsageErrors(t *testing.T) {
	h := newRig(t, baseConfig)
	// Not a git repository.
	for _, sub := range []string{"status", "run"} {
		if rc, out := h.run("gate", sub); rc != 2 || !strings.Contains(out, "not inside a git work tree") {
			t.Fatalf("gate %s outside git: rc=%d\n%s", sub, rc, out)
		}
	}
	// A repository without heavyq.toml.
	repo := gitRepo(t, h)
	if rc, out := h.run("gate", "status"); rc != 2 || !strings.Contains(out, "no heavyq.toml in") {
		t.Fatalf("no heavyq.toml: rc=%d\n%s", rc, out)
	}
	write := func(body string) {
		if err := os.WriteFile(filepath.Join(repo, "heavyq.toml"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("")
	if rc, out := h.run("gate", "status"); rc != 2 || !strings.Contains(out, "defines no gates") {
		t.Fatalf("empty file: rc=%d\n%s", rc, out)
	}
	write("[[gate]]\nname = \"a\"\nrun = [\"true\"]\n[[gate]]\nname = \"b\"\nrun = [\"true\"]\n")
	if rc, out := h.run("gate", "status"); rc != 2 || !strings.Contains(out, `has no gate "ready"; name one of: a, b`) {
		t.Fatalf("ambiguous: rc=%d\n%s", rc, out)
	}
	if rc, out := h.run("gate", "status", "nope"); rc != 2 || !strings.Contains(out, `no gate "nope"`) {
		t.Fatalf("unknown name: rc=%d\n%s", rc, out)
	}
	write("[[gate]]\nname = \"a\"\nrun = [\"true\"]\nbogus = 1\n")
	if rc, out := h.run("gate", "status"); rc != 2 || !strings.Contains(out, "unknown key") {
		t.Fatalf("invalid file: rc=%d\n%s", rc, out)
	}
	for _, args := range [][]string{{"gate"}, {"gate", "frobnicate"}, {"gate", "status", "a", "b"}, {"gate", "run", "--bogus"}} {
		if rc, _ := h.run(args...); rc != 2 {
			t.Fatalf("heavyq %v: rc=%d, want 2", args, rc)
		}
	}
	// A remote the config does not know is a usage error from the run, not a skipped command.
	write("[[gate]]\nname = \"a\"\nrun = [\"true\"]\nremote = \"nowhere\"\n")
	if rc, out := h.run("gate", "run"); rc != 1 || !strings.Contains(out, `--remote "nowhere"`) {
		t.Fatalf("unknown remote: rc=%d\n%s", rc, out)
	}
}

// The only gate is the default when it is not called "ready"; the name and the
// flags may come in any order; and from a subdirectory the gate still runs at
// the repository root (so its runs match what status looks for).
func TestE2EGateOnlyGateFromASubdirectory(t *testing.T) {
	h := newRig(t, baseConfig)
	repo := gitRepo(t, h)
	body := "[[gate]]\nname = \"check\"\nrun = [\"test -f marker.txt\"]\nlight = true\n"
	if err := os.WriteFile(filepath.Join(repo, "heavyq.toml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, h, repo, "add", ".")
	gitIn(t, h, repo, "commit", "-q", "-m", "gate")
	sub := filepath.Join(repo, "pkg", "x")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	h.cwd = sub
	if rc, out := h.run("gate", "run", "--lane", "gl"); rc != 0 {
		t.Fatalf("rc=%d\n%s", rc, out)
	}
	if r := h.lastRecord(); r.Subdir != "" || r.Gate != "check" || !r.Light || r.Lane != "gl" {
		t.Fatalf("the gate must run at the repository root, light as configured: %+v", r)
	}
	if rc, g := h.gateJSON("--json", "check"); rc != 0 || !g.Proven || g.Gate != "check" {
		t.Fatalf("rc=%d %+v", rc, g)
	}
}

func TestE2EWhyMentionsTheGate(t *testing.T) {
	h := newRig(t, baseConfig)
	gateRepo(t, h, "")
	rc, out := h.run("why")
	if rc != 0 || !strings.Contains(out, "The ready gate is not proven on this tree (tree ") || !strings.Contains(out, "0 failed, 3 missing") {
		t.Fatalf("why before: rc=%d\n%s", rc, out)
	}
	if rc, o := h.run("gate", "run"); rc != 0 {
		t.Fatalf("rc=%d\n%s", rc, o)
	}
	if rc, out = h.run("why"); rc != 0 || !strings.Contains(out, "The ready gate is proven on this tree (tree ") {
		t.Fatalf("why after: rc=%d\n%s", rc, out)
	}
}

// Commands whose ledger spelling would be cut cannot be matched exactly.
func TestE2EGateLongCommandIsReportedUnprovable(t *testing.T) {
	h := newRig(t, baseConfig)
	repo := gitRepo(t, h)
	long := "true " + strings.Repeat("#", 300)
	if err := os.WriteFile(filepath.Join(repo, "heavyq.toml"), []byte("[[gate]]\nname = \"ready\"\nrun = ['"+long+"']\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, h, repo, "add", ".")
	gitIn(t, h, repo, "commit", "-q", "-m", "gate")
	rc, out := h.run("gate", "run")
	if rc != 1 || !strings.Contains(out, "longer than the ledger keeps") {
		t.Fatalf("rc=%d\n%s", rc, out)
	}
}
