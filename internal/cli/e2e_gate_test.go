package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LysCongThanh/heavyq/internal/proof"
)

// gateRepo makes the rig's git repository (see gitRepo) carry a committed
// heavyq.toml whose gate "ready" has three commands. Each counts its runs in a
// file outside the repository, so the work tree stays clean. The middle one
// fails whenever marker.txt differs from its committed content.
func gateRepo(t *testing.T, h *rig, extra string) string {
	t.Helper()
	repo := gitRepo(t, h)
	count := func(tag string) string { return fmt.Sprintf("echo run >> %s/count-%s", h.root, tag) }
	toml := fmt.Sprintf(`[[gate]]
name = "ready"
run  = ['%s; echo A', '%s; test "$(cat marker.txt)" = from-commit', '%s; echo C']
prio = 3
%s`, count("a"), count("b"), count("c"), extra)
	if err := os.WriteFile(filepath.Join(repo, "heavyq.toml"), []byte(toml), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, h, repo, "add", "heavyq.toml")
	gitIn(t, h, repo, "commit", "-q", "-m", "add gate")
	return repo
}

func (h *rig) edit(repo, content string) {
	h.t.Helper()
	if err := os.WriteFile(filepath.Join(repo, "marker.txt"), []byte(content), 0o644); err != nil {
		h.t.Fatal(err)
	}
}

// runSplit runs heavyq and returns stdout and stderr apart.
func (h *rig) runSplit(args ...string) (rc int, stdout, stderr string) {
	h.t.Helper()
	cmd := exec.Command(binPath, args...)
	cmd.Dir, cmd.Env = h.cwd, h.env
	var o, e bytes.Buffer
	cmd.Stdout, cmd.Stderr = &o, &e
	if err := cmd.Start(); err != nil {
		h.t.Fatal(err)
	}
	rc = processExit(cmd.Wait())
	return rc, o.String(), e.String()
}

func (h *rig) gateJSON(args ...string) (int, proof.Gate) {
	h.t.Helper()
	rc, out, errOut := h.runSplit(append([]string{"gate", "status", "--json"}, args...)...)
	var g proof.Gate
	if err := json.Unmarshal([]byte(out), &g); err != nil {
		h.t.Fatalf("gate status --json is not JSON: %v\nstdout: %s\nstderr: %s", err, out, errOut)
	}
	return rc, g
}

func (h *rig) counts() string {
	return fmt.Sprintf("%d%d%d", h.runs("a"), h.runs("b"), h.runs("c"))
}

func TestE2EGateProveEditRunRevert(t *testing.T) {
	h := newRig(t, baseConfig)
	repo := gateRepo(t, h, "")

	rc, g := h.gateJSON()
	if rc != 1 || g.Proven || len(g.Commands) != 3 || g.Commands[0].State != proof.Missing || g.Tree == "" || g.Dirty {
		t.Fatalf("before any run: rc=%d %+v", rc, g)
	}
	if rc, out := h.run("gate", "run"); rc != 0 || h.counts() != "111" {
		t.Fatalf("gate run rc=%d counts=%s\n%s", rc, h.counts(), out)
	}
	rc, g = h.gateJSON("ready")
	if rc != 0 || !g.Proven || g.Commands[1].State != proof.Proven || g.Commands[1].Lane != "app" || g.Commands[1].Exit == nil {
		t.Fatalf("after gate run: rc=%d %+v", rc, g)
	}
	cleanTree := g.Tree
	if rc, out := h.run("gate", "status"); rc != 0 || !strings.Contains(out, "gate ready: PROVEN") {
		t.Fatalf("status text rc=%d\n%s", rc, out)
	}
	// Every run of the gate went through the queue with the gate's tags.
	if r := h.lastRecord(); r.Gate != "ready" || r.Prio != 3 || r.Cache != "miss" || r.Cmd != "sh -c "+g.Commands[2].Cmd {
		t.Fatalf("ledger record of a gate command: %+v", r)
	}

	// An edit flips it to unproven: a different tree has no proof.
	h.edit(repo, "from-commit\nedited\n")
	if rc, g = h.gateJSON(); rc != 1 || g.Proven || !g.Dirty || g.Tree == cleanTree {
		t.Fatalf("after edit: rc=%d %+v", rc, g)
	}
	// ... the failing middle command fails on the edited file, so the run stops.
	rc, out := h.run("gate", "run")
	if rc != 1 || h.counts() != "221" || !strings.Contains(out, "FAILED (exit 1)") || !strings.Contains(out, "not run") {
		t.Fatalf("gate run on edited tree rc=%d counts=%s\n%s", rc, h.counts(), out)
	}
	// Undoing the edit restores the clean tree id, which is still proven:
	// nothing runs again.
	h.edit(repo, "from-commit\n")
	rc, g = h.gateJSON()
	if rc != 0 || !g.Proven || g.Tree != cleanTree || g.Dirty {
		t.Fatalf("after revert: rc=%d %+v", rc, g)
	}
	rc, out = h.run("gate", "run")
	if rc != 0 || h.counts() != "221" || strings.Count(out, "proven:") != 3 {
		t.Fatalf("gate run after revert must skip everything: rc=%d counts=%s\n%s", rc, h.counts(), out)
	}

	// An untracked file is another tree again: the whole gate runs (a dirty
	// tree is never cached) and the dirty tree is proven too.
	if err := os.WriteFile(filepath.Join(repo, "scratch.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if rc, out := h.run("gate", "run"); rc != 0 || h.counts() != "332" {
		t.Fatalf("gate run on a dirty tree rc=%d counts=%s\n%s", rc, h.counts(), out)
	}
	if rc, g = h.gateJSON(); rc != 0 || !g.Proven || !g.Dirty || g.Tree == cleanTree {
		t.Fatalf("dirty tree proven: rc=%d %+v", rc, g)
	}
	if err := os.Remove(filepath.Join(repo, "scratch.txt")); err != nil {
		t.Fatal(err)
	}
	if rc, g = h.gateJSON(); rc != 0 || g.Tree != cleanTree {
		t.Fatalf("clean again: rc=%d %+v", rc, g)
	}
}

// With the ledger gone, a revert is still instant: the result cache answers.
func TestE2EGateRerunAfterLedgerLossIsAnInstantCacheHit(t *testing.T) {
	h := newRig(t, baseConfig)
	gateRepo(t, h, "")
	if rc, out := h.run("gate", "run"); rc != 0 {
		t.Fatalf("rc=%d\n%s", rc, out)
	}
	if err := os.Remove(filepath.Join(h.root, "history.jsonl")); err != nil {
		t.Fatal(err)
	}
	if rc, _ := h.gateJSON(); rc != 1 {
		t.Fatalf("a gate with no ledger is not proven, rc=%d", rc)
	}
	rc, out := h.run("gate", "run")
	if rc != 0 || h.counts() != "111" || strings.Count(out, "cached result from") != 3 {
		t.Fatalf("rc=%d counts=%s\n%s", rc, h.counts(), out)
	}
	rc, g := h.gateJSON()
	if rc != 0 || !g.Proven || g.Commands[0].Cache != "hit" {
		t.Fatalf("a cache hit proves the tree: rc=%d %+v", rc, g)
	}
}

func TestE2EGateFailingCommandShowsFailedAndKeepGoingRunsTheRest(t *testing.T) {
	h := newRig(t, baseConfig)
	repo := gateRepo(t, h, "")
	h.edit(repo, "broken\n")
	rc, out := h.run("gate", "run", "ready")
	if rc != 1 || h.counts() != "110" {
		t.Fatalf("rc=%d counts=%s\n%s", rc, h.counts(), out)
	}
	rc, g := h.gateJSON()
	if rc != 1 || g.Proven || g.Commands[0].State != proof.Proven || g.Commands[1].State != proof.Failed ||
		g.Commands[1].Exit == nil || *g.Commands[1].Exit != 1 || g.Commands[2].State != proof.Missing {
		t.Fatalf("status after failure: rc=%d %+v", rc, g)
	}
	rc, text := h.run("gate", "status")
	if rc != 1 || !strings.Contains(text, "NOT PROVEN") || !strings.Contains(text, "(exit 1)") || !strings.Contains(text, "1 failed, 1 missing") {
		t.Fatalf("status text rc=%d\n%s", rc, text)
	}
	if rc, out = h.run("gate", "run", "--keep-going"); rc != 1 || h.counts() != "121" {
		t.Fatalf("--keep-going rc=%d counts=%s\n%s", rc, h.counts(), out)
	}
	if _, g = h.gateJSON(); g.Commands[2].State != proof.Proven || g.Commands[1].State != proof.Failed {
		t.Fatalf("%+v", g)
	}
}

func TestE2EGateParallelRunsEveryCommandAndProves(t *testing.T) {
	h := newRig(t, parallelConfig)
	gateRepo(t, h, "")
	rc, out := h.run("gate", "run", "--parallel", "--lane", "gl")
	if rc != 0 || h.counts() != "111" {
		t.Fatalf("rc=%d counts=%s\n%s", rc, h.counts(), out)
	}
	if rc, g := h.gateJSON(); rc != 0 || !g.Proven || g.Commands[0].Lane != "gl" {
		t.Fatalf("rc=%d %+v", rc, g)
	}
}
