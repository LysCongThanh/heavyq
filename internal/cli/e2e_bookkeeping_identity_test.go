package cli

import (
	"os"
	"path/filepath"
	"testing"
)

// heavyq writes to its own anchor file (a ticked item, a decision): that must
// not turn a proven gate stale, whether the tick is uncommitted or committed.
// Editing heavyq.toml or code still does.
func TestE2EBookkeepingWritesKeepTheGateProven(t *testing.T) {
	h := newRig(t, baseConfig)
	repo := gateRepo(t, h, "")
	anchor := filepath.Join(repo, ".heavyq", "anchor.md")
	put := func(path, content string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	put(anchor, "# goal\n\n- [ ] ship it\n")
	put(filepath.Join(repo, ".heavyq", "decisions", "d1.md"), "chose A\n")
	gitIn(t, h, repo, "add", ".heavyq")
	gitIn(t, h, repo, "commit", "-q", "-m", "anchor")

	if rc, out := h.run("gate", "run"); rc != 0 || h.counts() != "111" {
		t.Fatalf("gate run rc=%d counts=%s\n%s", rc, h.counts(), out)
	}
	rc, g := h.gateJSON("ready")
	if rc != 0 || !g.Proven || g.Dirty {
		t.Fatalf("proven before the tick: rc=%d %+v", rc, g)
	}
	proven := g.Tree

	// The tick, uncommitted, and a new decision: still proven, same tree id.
	put(anchor, "# goal\n\n- [x] ship it\n")
	put(filepath.Join(repo, ".heavyq", "decisions", "d2.md"), "chose B\n")
	if rc, g = h.gateJSON("ready"); rc != 0 || !g.Proven || g.Dirty || g.Tree != proven {
		t.Fatalf("after an uncommitted tick: rc=%d %+v (want tree %s)", rc, g, proven)
	}
	if rc, out := h.run("gate", "run"); rc != 0 || h.counts() != "111" {
		t.Fatalf("gate run after the tick must skip everything: rc=%d counts=%s\n%s", rc, h.counts(), out)
	}

	// Committing the tick: still proven.
	gitIn(t, h, repo, "add", ".heavyq")
	gitIn(t, h, repo, "commit", "-q", "-m", "tick")
	if rc, g = h.gateJSON("ready"); rc != 0 || !g.Proven || g.Dirty || g.Tree != proven {
		t.Fatalf("after the committed tick: rc=%d %+v (want tree %s)", rc, g, proven)
	}

	// Code and gate edits still void the proof.
	h.edit(repo, "from-commit\nedited\n")
	if rc, g = h.gateJSON("ready"); rc != 1 || g.Proven || g.Tree == proven {
		t.Fatalf("after a code edit: rc=%d %+v", rc, g)
	}
	h.edit(repo, "from-commit\n")
	put(filepath.Join(repo, "heavyq.toml"), "[[gate]]\nname = \"ready\"\nrun = ['true']\n")
	if rc, g = h.gateJSON("ready"); rc != 1 || g.Proven || g.Tree == proven {
		t.Fatalf("after a heavyq.toml edit: rc=%d %+v", rc, g)
	}
}
