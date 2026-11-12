package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LysCongThanh/heavyq/internal/proof"
)

// A gate that declares `paths` is proven by the files they name (#141): a commit that touches only documentation
// keeps its proof, for the gate and for the item that rests on it, and a commit that touches a Go file voids it.
func TestE2EDocsCommitKeepsScopedProof(t *testing.T) {
	h := newRig(t, baseConfig)
	app, _ := itemRepo(t, h, "# Goal: paths\n\n- [ ] ship: ship it `proof: gate:web`\n", `paths = ["**/*.go", "go.mod"]`,
		map[string]string{"main.go": "package main\n", "go.mod": "module m\n", "docs/guide.md": "# guide\n"})
	status := func() proof.Gate {
		t.Helper()
		rc, out, errOut := h.inDir(app, nil, "gate", "status", "web", "--json")
		var g proof.Gate
		if err := json.Unmarshal([]byte(out), &g); err != nil {
			t.Fatalf("gate status --json rc=%d: %v\n%s%s", rc, err, out, errOut)
		}
		return g
	}
	commit := func(msg string, files map[string]string) {
		t.Helper()
		for name, content := range files {
			p := filepath.Join(app, name)
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		gitIn(t, h, app, "add", "-A")
		gitIn(t, h, app, "commit", "-q", "-m", msg)
	}

	if g := status(); g.Proven || !strings.HasPrefix(g.Tree, "paths-") {
		t.Fatalf("before any run, the tree id is the files the paths name: %+v", g)
	}
	h.mustRun(app, 0, "gate", "run", "web")
	proved := status()
	if !proved.Proven || h.runs("w") != 1 {
		t.Fatalf("after gate run: %+v, %d runs", proved, h.runs("w"))
	}
	if st := h.itemRowsJSON(app)["ship"].State; st != "proven" {
		t.Fatalf("the item rests on the gate: state %q", st)
	}
	// A result replayed from the cache is recorded on the scoped id as well (the cache itself is keyed by the whole
	// tree), so it counts for the gate like the run it repeats.
	cmd := "echo run >> " + h.root + "/count-w; echo web ok"
	h.mustRun(app, 0, append([]string{"3", "--gate", "web", "--cache", "--"}, proof.Argv(cmd)...)...)
	if r := h.lastRecord(); r.Cache != "hit" || r.Tree != proved.Tree || h.runs("w") != 1 {
		t.Fatalf("replay: cache %q, tree %q (want %q), %d runs", r.Cache, r.Tree, proved.Tree, h.runs("w"))
	}

	// Documentation only: committed, and then edited again without a commit. Nothing is proven again, nothing runs.
	commit("docs", map[string]string{"docs/guide.md": "# guide, edited\n", "CHANGELOG.md": "- docs\n"})
	if g := status(); !g.Proven || g.Tree != proved.Tree || g.Dirty {
		t.Fatalf("a docs-only commit lost the proof: %+v, want tree %s", g, proved.Tree)
	}
	if err := os.WriteFile(filepath.Join(app, "docs", "guide.md"), []byte("# guide, again\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if g := status(); !g.Proven || g.Tree != proved.Tree || g.Dirty {
		t.Fatalf("a docs edit made the gate unproven or dirty: %+v", g)
	}
	if st := h.itemRowsJSON(app)["ship"].State; st != "proven" {
		t.Fatalf("a docs-only change made the item %q", st)
	}
	if out := h.mustRun(app, 0, "gate", "run", "web"); h.runs("w") != 1 || !strings.Contains(out, "proven:") {
		t.Fatalf("gate run after a docs-only commit ran something (%d runs):\n%s", h.runs("w"), out)
	}
	gitIn(t, h, app, "checkout", "-q", "--", "docs/guide.md")

	// A Go file: the proof is void until the gate runs on the new code.
	commit("code", map[string]string{"main.go": "package main\n\nfunc main() {}\n"})
	if g := status(); g.Proven || g.Tree == proved.Tree {
		t.Fatalf("a code commit kept the proof: %+v", g)
	}
	if st := h.itemRowsJSON(app)["ship"].State; st != "stale" {
		t.Fatalf("a code commit should make the item stale, got %q", st)
	}
	h.mustRun(app, 0, "gate", "run", "web")
	if g := status(); !g.Proven || h.runs("w") != 2 {
		t.Fatalf("after the second gate run: %+v, %d runs", g, h.runs("w"))
	}
}

// paths that match no committed file prove nothing, instead of everything: the gate refuses to run.
func TestE2EGatePathsThatMatchNothingProveNothing(t *testing.T) {
	h := newRig(t, baseConfig)
	app, _ := itemRepo(t, h, "# Goal: paths\n", `paths = ["**/*.rs"]`, map[string]string{"main.go": "package main\n"})
	rc, out, errOut := h.inDir(app, nil, "gate", "run", "web")
	if rc != 1 || h.runs("w") != 0 || !strings.Contains(out+errOut, "match no file") {
		t.Fatalf("rc=%d, %d runs\n%s%s", rc, h.runs("w"), out, errOut)
	}
}
