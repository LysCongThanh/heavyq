package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// siblingItemRepo commits a gate "ready" with `with = ["lib"]` and an anchor
// item proved by it.
func siblingItemRepo(t *testing.T, h *rig, app string) {
	t.Helper()
	files := map[string]string{
		"heavyq.toml":       fmt.Sprintf("[[gate]]\nname = \"ready\"\nrun = ['cat ../lib/a.txt && echo run >> %s/count-a']\nwith = [\"lib\"]\n", h.root),
		".heavyq/anchor.md": "# Goal: siblings\n\n- [ ] lib-item: needs the sibling `proof: gate:ready`\n",
	}
	for name, content := range files {
		p := filepath.Join(app, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	gitIn(t, h, app, "add", ".")
	gitIn(t, h, app, "commit", "-q", "-m", "gate with a sibling")
}

// An item proved by a gate with `with` siblings is judged on the same subject
// as the gate (its sibling commits are part of the run_id), so once the gate
// is proven the item can be done.
func TestE2EItemProvedByAGateWithSiblings(t *testing.T) {
	h, app, _ := withRig(t, baseConfig)
	siblingItemRepo(t, h, app)
	if rc, out := h.run("gate", "run"); rc != 0 {
		t.Fatalf("gate run rc=%d\n%s", rc, out)
	}
	if rc, g := h.gateJSON(); rc != 0 || !g.Proven {
		t.Fatalf("gate status: rc=%d %+v", rc, g)
	}
	rc, out, errOut := h.inDir(app, nil, "item", "done", "lib-item")
	if rc != 0 || !strings.Contains(out, "ticked [x]") {
		t.Fatalf("item done after a proven gate: rc=%d\n%s\n%s", rc, out, errOut)
	}
}

// A sibling with uncommitted edits is not the commit a proof names: a local
// run of the gate would read the edits, and nothing ran them. The gate is not
// proven while they stay, and says why.
func TestE2EDirtySiblingLeavesTheGateUnproven(t *testing.T) {
	h, app, lib := withRig(t, baseConfig)
	siblingGate(t, h, app, "")
	if rc, out := h.run("gate", "run"); rc != 0 {
		t.Fatalf("gate run rc=%d\n%s", rc, out)
	}
	if err := os.WriteFile(filepath.Join(lib, "a.txt"), []byte("an edit nobody ran\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rc, g := h.gateJSON()
	if rc != 1 || g.Proven || !strings.Contains(g.Reason, "lib") {
		t.Fatalf("a dirty sibling must not keep the gate proven: rc=%d %+v", rc, g)
	}
	gitIn(t, h, lib, "checkout", "--", "a.txt")
	if rc, g := h.gateJSON(); rc != 0 || !g.Proven {
		t.Fatalf("clean again, the proof of the pinned commit holds: rc=%d %+v", rc, g)
	}
}
