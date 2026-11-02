package cli

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LysCongThanh/heavyq/internal/history"
	"github.com/LysCongThanh/heavyq/internal/proof"
)

// sizeAnchor has one item of each kind the size gate tells apart. Line 2 is "big".
const sizeAnchor = "# Goal: size gate\n" +
	"- [ ] big: too long to finish in a lease `proof: echo big-proof`\n" +
	"- [ ] noproof: nothing could prove it\n" +
	"- [ ] small: quick `proof: echo small-proof`\n" +
	"- [ ] fresh: its proof never passed here `proof: echo fresh-proof`\n"

// seedPassingRun writes a passing run of the shell command cmd that took secs
// into the rig's ledger, as `heavyq` would have.
func seedPassingRun(t *testing.T, h *rig, cmd string, secs float64) {
	t.Helper()
	path := ""
	for _, e := range h.env {
		if v, ok := strings.CutPrefix(e, "HEAVYQ_HISTORY="); ok {
			path = v
		}
	}
	argv := proof.Argv(cmd)
	rec := history.Record{End: time.Now().Add(-time.Hour), Machine: "local", Lane: "app", Prio: 3, RunSecs: secs,
		Cmd: strings.Join(argv, " "), CmdHash: history.CmdHash(argv)}
	if err := history.Append(path, rec); err != nil {
		t.Fatal(err)
	}
}

// sizeRepo is a repository with sizeAnchor where "big" takes 700 s (3 x 700 s
// is over the 30 min lease) and "small" 60 s.
func sizeRepo(t *testing.T, anchor, toml string) (*rig, string) {
	t.Helper()
	h := newRig(t, baseConfig)
	app, _ := itemRepo(t, h, anchor, toml, nil)
	seedPassingRun(t, h, "echo big-proof", 700)
	seedPassingRun(t, h, "echo small-proof", 60)
	return h, app
}

func TestE2EClaimRefusesOversizedItem(t *testing.T) {
	h, app := sizeRepo(t, sizeAnchor, "")

	rc, out, errOut := h.inDir(app, nil, "item", "claim", "big")
	if rc != 1 || out != "" {
		t.Fatalf("claim big: rc=%d out=%q err=%q, want 1 and nothing on stdout", rc, out, errOut)
	}
	for _, want := range []string{"item big is too big for one 30m lease", "about 12m (p50), times 3 iterations is 35m, over the 30m budget",
		"next: split big into ≤3 items, each with a faster proof: ${EDITOR:-vi} +2 .heavyq/anchor.md", "heavyq item claim big --force"} {
		if !strings.Contains(errOut, want) {
			t.Errorf("refusal lacks %q:\n%s", want, errOut)
		}
	}
	if st := h.itemRowsJSON(app)["big"].State; st != "open" {
		t.Errorf("refused item is %q, want open (nothing was leased)", st)
	}

	rc, out, _ = h.inDir(app, nil, "item", "claim", "big", "--json")
	var doc struct {
		Refused sizeRefusal `json:"refused"`
	}
	if err := json.Unmarshal([]byte(out), &doc); rc != 1 || err != nil {
		t.Fatalf("claim big --json: rc=%d err=%v\n%s", rc, err, out)
	}
	if r := doc.Refused; r.Item != "big" || r.Code != "oversized" || r.ProofP50S != 700 || r.Iterations != 3 ||
		r.ExpectedS != 2100 || r.BudgetS != 1800 || !strings.HasPrefix(r.Next, "split big into ≤3 items") {
		t.Errorf("refused = %+v", r)
	}

	if rc, out, errOut = h.inDir(app, nil, "item", "claim", "big", "--force"); rc != 0 || !strings.Contains(out, "claimed big") {
		t.Fatalf("claim big --force: rc=%d out=%q err=%q", rc, out, errOut)
	}
}

func TestE2EClaimSizeGateNoProofAndUnknownDuration(t *testing.T) {
	h, app := sizeRepo(t, sizeAnchor, "")

	rc, _, errOut := h.inDir(app, nil, "item", "claim", "noproof")
	if rc != 1 || !strings.Contains(errOut, "item noproof has no proof") || !strings.Contains(errOut, "next: add `proof: <command>` to noproof") {
		t.Fatalf("claim noproof: rc=%d\n%s", rc, errOut)
	}
	if rc, _, errOut = h.inDir(app, nil, "item", "claim", "noproof", "--force"); rc != 0 {
		t.Fatalf("claim noproof --force: rc=%d\n%s", rc, errOut)
	}

	// A proof with no passing run in the ledger has an unknown size: allowed, with a warning.
	rc, out, errOut := h.inDir(app, nil, "item", "claim", "fresh", "--json")
	var c claimJSON
	if err := json.Unmarshal([]byte(out), &c); rc != 0 || err != nil || !strings.Contains(c.Warning, "no known duration") ||
		!strings.Contains(errOut, "warning: item fresh: no known duration") {
		t.Fatalf("claim fresh: rc=%d err=%v out=%s stderr=%s", rc, err, out, errOut)
	}

	// Claiming the item again renews it and is not judged again.
	if rc, _, errOut = h.inDir(app, nil, "item", "claim", "fresh"); rc != 0 || strings.Contains(errOut, "warning") {
		t.Errorf("renewing fresh: rc=%d\n%s", rc, errOut)
	}
}

func TestE2EClaimNextSkipsOversizedItems(t *testing.T) {
	h, app := sizeRepo(t, sizeAnchor, "")

	// big and noproof come first in the anchor and are passed over.
	rc, out, errOut := h.inDir(app, nil, "item", "claim", "--next", "--json")
	var c claimJSON
	if err := json.Unmarshal([]byte(out), &c); rc != 0 || err != nil || c.Item != "small" {
		t.Fatalf("claim --next: rc=%d err=%v out=%s\n%s", rc, err, out, errOut)
	}
	if !strings.Contains(errOut, "passed over 2 item(s)") {
		t.Errorf("no note about the skipped items:\n%s", errOut)
	}

	// Another lane takes the one left with a warning; then only refused items are left.
	laneB := filepath.Join(h.root, "lane-b")
	if rc, out, errOut = h.inDir(laneB, nil, "item", "claim", "--next", "--json"); rc != 0 || !strings.Contains(out, `"item": "fresh"`) {
		t.Fatalf("claim --next in lane-b: rc=%d\n%s\n%s", rc, out, errOut)
	}
	rc, out, errOut = h.inDir(app, []string{"HEAVYQ_LANE=third"}, "item", "claim", "--next", "--json")
	var doc struct {
		Refused sizeRefusal `json:"refused"`
	}
	if err := json.Unmarshal([]byte(out), &doc); rc != 1 || err != nil || doc.Refused.Item != "big" ||
		len(doc.Refused.Skipped) != 1 || doc.Refused.Skipped[0] != "noproof" {
		t.Fatalf("claim --next with only refused items: rc=%d err=%v\n%s\n%s", rc, err, out, errOut)
	}
	if rc, out, errOut = h.inDir(app, []string{"HEAVYQ_LANE=third"}, "item", "claim", "--next", "--force"); rc != 0 || !strings.Contains(out, "claimed big") {
		t.Fatalf("claim --next --force: rc=%d\n%s\n%s", rc, out, errOut)
	}
}

func TestE2EClaimSizeGateReadsItemsSection(t *testing.T) {
	// One iteration: 700 s is far under the lease, so big is claimable.
	h, app := sizeRepo(t, sizeAnchor, "\n[items]\niterations = 1\n")
	if rc, out, errOut := h.inDir(app, nil, "item", "claim", "big"); rc != 0 {
		t.Fatalf("iterations = 1: rc=%d\n%s\n%s", rc, out, errOut)
	}

	// A smaller share of the lease: 60 s x 3 is over a tenth of 30 min.
	h, app = sizeRepo(t, sizeAnchor, "\n[items]\nmax_proof_share = 0.1\n")
	rc, _, errOut := h.inDir(app, nil, "item", "claim", "small")
	if rc != 1 || !strings.Contains(errOut, "over the 3m budget") {
		t.Fatalf("max_proof_share = 0.1: rc=%d\n%s", rc, errOut)
	}
}
