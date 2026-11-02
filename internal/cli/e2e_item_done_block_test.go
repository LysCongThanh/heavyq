package cli

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LysCongThanh/heavyq/internal/snapshot"
)

func TestE2EItemDoneRefusesThenSucceedsAndTicksOnlyThatBox(t *testing.T) {
	h := newRig(t, baseConfig)
	app, _ := itemRepo(t, h, itemAnchor, "", nil)
	anchorPath := filepath.Join(app, ".heavyq", "anchor.md")
	orig := mustRead(t, anchorPath)

	if rc, _, errOut := h.inDir(app, nil, "item", "claim", "web-item"); rc != 0 {
		t.Fatalf("claim rc=%d\n%s", rc, errOut)
	}
	rc, _, errOut := h.inDir(app, nil, "item", "done", "web-item")
	if rc != 1 || !strings.Contains(errOut, "not proven on this code") || !strings.Contains(errOut, "next: heavyq gate run web") {
		t.Fatalf("done before any proof: rc=%d\n%s", rc, errOut)
	}
	if mustRead(t, anchorPath) != orig {
		t.Fatal("a refused done must not touch the anchor")
	}

	if rc, out, errOut := h.inDir(app, nil, "gate", "run", "web"); rc != 0 {
		t.Fatalf("gate run rc=%d\n%s%s", rc, out, errOut)
	}
	if r := h.itemRowsJSON(app)["web-item"]; r.State != "proven" || r.Lane != "app" || len(r.Commit) != 7 || r.At.IsZero() {
		t.Fatalf("after the gate: %+v", r)
	}
	if rc, out, errOut := h.inDir(app, nil, "item", "done", "web-item"); rc != 0 || !strings.Contains(out, "ticked [x]") {
		t.Fatalf("done rc=%d\n%s%s", rc, out, errOut)
	}
	want := strings.Replace(orig, "- [ ] web-item", "- [x] web-item", 1)
	if got := mustRead(t, anchorPath); got != want {
		t.Fatalf("exactly one checkbox may change.\ngot:  %q\nwant: %q", got, want)
	}
	// The lease is gone. The tick only touched heavyq's own bookkeeping (the anchor
	// file is excluded from the tree identity, #71), so the proof still holds.
	if r := h.itemRowsJSON(app)["web-item"]; r.Lane != "" || !r.Done || r.State != "proven" {
		t.Fatalf("after done: %+v", r)
	}
	if ev := mustRead(t, filepath.Join(app, ".git", "heavyq", "events.jsonl")); !strings.Contains(ev, `"op":"done"`) {
		t.Fatalf("events: %s", ev)
	}

	// --run runs the proof (a command here) through the queue, tagged with the item.
	if rc, out, errOut := h.inDir(app, nil, "item", "done", "cmd-item", "--run"); rc != 0 || !strings.Contains(out, "done cmd-item") {
		t.Fatalf("done --run rc=%d\n%s%s", rc, out, errOut)
	}
	if n := h.runs("c"); n != 1 {
		t.Fatalf("the proof ran %d times", n)
	}
	if !strings.Contains(h.read("history.jsonl"), `"item":"cmd-item"`) {
		t.Fatalf("the ledger does not name the item:\n%s", h.read("history.jsonl"))
	}
	want = strings.Replace(want, "- [ ] cmd-item", "- [x] cmd-item", 1)
	if got := mustRead(t, anchorPath); got != want {
		t.Fatalf("exactly one more checkbox may change.\ngot:  %q\nwant: %q", got, want)
	}
	// No proof declared: refused, whatever else is true.
	if rc, _, errOut := h.inDir(app, nil, "item", "done", "plain-item"); rc != 1 || !strings.Contains(errOut, "declares no proof") {
		t.Fatalf("plain-item rc=%d\n%s", rc, errOut)
	}
}

func TestE2EItemPlanFileItemsAreClaimedAndDoneButNeverRewritten(t *testing.T) {
	h := newRig(t, baseConfig)
	plan := "# Phase 1\n\n- [ ] Write the docs  `proof: gate:web`\n- [ ] Echo thing `proof: echo run >> {ROOT}/count-p; echo ok`\n- [ ] No proof\n"
	app, _ := itemRepo(t, h, "# Goal: g\n- [ ] a: x\n", "\n[anchor]\nextra = [\"plans/*/phase-*.md\"]\n",
		map[string]string{"plans/p1/phase-01.md": strings.ReplaceAll(plan, "{ROOT}", h.root)})
	planPath, anchorPath := filepath.Join(app, "plans", "p1", "phase-01.md"), filepath.Join(app, ".heavyq", "anchor.md")
	planOrig, anchorOrig := mustRead(t, planPath), mustRead(t, anchorPath)

	var docs, echo, none string
	for id, r := range h.itemRowsJSON(app) {
		switch {
		case !r.ReadOnly:
		case strings.HasSuffix(id, "write-the-docs"):
			docs = id
		case strings.HasSuffix(id, "echo-thing"):
			echo = id
		case strings.HasSuffix(id, "no-proof"):
			none = id
		}
	}
	if docs == "" || echo == "" || none == "" {
		t.Fatalf("plan items not found: %q %q %q", docs, echo, none)
	}
	if rc, _, errOut := h.inDir(app, nil, "item", "claim", docs); rc != 0 {
		t.Fatalf("claim %s rc=%d\n%s", docs, rc, errOut)
	}
	if rc, _, _ := h.inDir(app, nil, "item", "done", docs); rc != 1 {
		t.Fatalf("done before proof rc=%d", rc)
	}
	if rc, _, errOut := h.inDir(app, nil, "gate", "run", "web"); rc != 0 {
		t.Fatalf("gate run rc=%d\n%s", rc, errOut)
	}
	if rc, out, errOut := h.inDir(app, nil, "item", "done", docs); rc != 0 || !strings.Contains(out, "never rewritten") {
		t.Fatalf("done rc=%d\n%s%s", rc, out, errOut)
	}
	// An id with "/" is tagged in the ledger with ":" instead.
	if rc, out, errOut := h.inDir(app, nil, "item", "done", echo, "--run"); rc != 0 {
		t.Fatalf("done --run rc=%d\n%s%s", rc, out, errOut)
	}
	if !strings.Contains(h.read("history.jsonl"), `"item":"plans-p1-phase-01:`) {
		t.Fatalf("ledger:\n%s", h.read("history.jsonl"))
	}
	if rc, _, errOut := h.inDir(app, nil, "item", "done", none); rc != 1 || !strings.Contains(errOut, "declares no proof") {
		t.Fatalf("no proof rc=%d\n%s", rc, errOut)
	}

	if mustRead(t, planPath) != planOrig || mustRead(t, anchorPath) != anchorOrig {
		t.Fatal("neither the plan file nor the anchor may be rewritten")
	}
	rows := h.itemRowsJSON(app)
	if r := rows[docs]; r.State != "proven" || !r.Done || !r.ReadOnly || r.Checked {
		t.Fatalf("a plan item marked done stays proven and unticked: %+v", r)
	}
	if rows[echo].State != "proven" || !rows[echo].Done {
		t.Fatalf("%+v", rows[echo])
	}
}

func TestE2EItemBlockShowsInStatusAndTowerUntilClaimed(t *testing.T) {
	h := newRig(t, baseConfig)
	app, laneB := itemRepo(t, h, itemAnchor, "", nil)
	if rc, _, errOut := h.inDir(app, nil, "item", "claim", "web-item"); rc != 0 {
		t.Fatalf("claim rc=%d\n%s", rc, errOut)
	}
	if rc, _, errOut := h.inDir(app, nil, "item", "block", "web-item", ""); rc != 2 || !strings.Contains(errOut, "say what is needed") {
		t.Fatalf("empty need rc=%d\n%s", rc, errOut)
	}
	if rc, out, errOut := h.inDir(app, nil, "item", "block", "web-item", "needs: design sign-off"); rc != 0 || !strings.Contains(out, "blocked web-item") {
		t.Fatalf("block rc=%d\n%s%s", rc, out, errOut)
	}
	r := h.itemRowsJSON(app)["web-item"]
	if r.State != "blocked" || r.Needs != "design sign-off" || r.Lane != "app" || !r.Expires.IsZero() {
		t.Fatalf("after block: %+v", r)
	}
	if _, out, _ := h.inDir(app, nil, "item", "ls"); !strings.Contains(out, "blocked (needs: design sign-off)") {
		t.Fatalf("ls:\n%s", out)
	}

	s := h.statusJSON() // from the work tree: no lane has run anything yet
	var chips []snapshot.ItemChip
	for _, li := range s.Items {
		if li.Lane == "app" {
			chips = li.Items
		}
	}
	if len(chips) != 1 || chips[0].ID != "web-item" || chips[0].State != "blocked" || chips[0].Needs != "design sign-off" {
		t.Fatalf("status items: %+v", s.Items)
	}
	ex := exceptionsOf(s, snapshot.KindBlocked)
	if len(ex) != 1 || ex[0].Lane != "app" || ex[0].Severity != snapshot.SeverityCritical || ex[0].Evidence.Item != "web-item" ||
		ex[0].Evidence.Detail != "design sign-off" || !strings.Contains(ex[0].Text, "design sign-off") ||
		ex[0].Action == nil || ex[0].Action.Command != "heavyq item claim web-item" {
		t.Fatalf("exceptions: %+v", s.Exceptions)
	}

	// --next never hands out a blocked item ...
	rc, out, errOut := h.inDir(laneB, nil, "item", "claim", "--next", "--json")
	if rc != 0 || !strings.Contains(out, `"item": "cmd-item"`) {
		t.Fatalf("claim --next rc=%d\n%s%s", rc, out, errOut)
	}
	// ... but a claim by name answers the block.
	if rc, _, errOut := h.inDir(laneB, nil, "item", "claim", "web-item"); rc != 0 {
		t.Fatalf("claim by name rc=%d\n%s", rc, errOut)
	}
	s = h.statusJSON()
	if ex := exceptionsOf(s, snapshot.KindBlocked); len(ex) != 0 {
		t.Fatalf("a claimed item is not blocked: %+v", ex)
	}
	if r := h.itemRowsJSON(app)["web-item"]; r.State != "claimed" || r.Lane != "lane-b" || r.Needs != "" {
		t.Fatalf("after claim: %+v", r)
	}
}

func TestE2EItemClaimedItemIsJudgedOnItsLanesTree(t *testing.T) {
	h := newRig(t, baseConfig)
	app, laneB := itemRepo(t, h, itemAnchor, "", nil)
	if err := writeFileIn(laneB, "extra.txt", "lane-b work\n"); err != nil {
		t.Fatal(err)
	}
	gitIn(t, h, laneB, "add", ".")
	gitIn(t, h, laneB, "commit", "-q", "-m", "lane-b work")
	head, err := exec.Command("git", "-C", laneB, "rev-parse", "--short=7", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}

	if rc, _, errOut := h.inDir(laneB, nil, "item", "claim", "web-item"); rc != 0 {
		t.Fatalf("claim rc=%d\n%s", rc, errOut)
	}
	if rc, _, errOut := h.inDir(laneB, nil, "gate", "run", "web"); rc != 0 {
		t.Fatalf("gate run in lane-b rc=%d\n%s", rc, errOut)
	}
	if rc, _, _ := h.inDir(app, nil, "gate", "status", "web"); rc != 1 {
		t.Fatalf("the main work tree has a different tree: it is not proven there (rc=%d)", rc)
	}
	// Seen from the main work tree: the claimed item is proven where its lane works, the rest is not.
	rows := h.itemRowsJSON(app)
	if r := rows["web-item"]; r.State != "proven" || r.Lane != "lane-b" || r.Commit != strings.TrimSpace(string(head)) {
		t.Fatalf("web-item: %+v (lane-b HEAD %s)", r, head)
	}
	if r := rows["ui-item"]; r.State != "stale" {
		t.Fatalf("an unclaimed item with the same gate is judged on this tree, where the gate passed on another: %+v", r)
	}
	if r := rows["cmd-item"]; r.State != "open" {
		t.Fatalf("cmd-item: %+v", r)
	}
}

func TestE2EItemAgentModeEndsWithOneNextLine(t *testing.T) {
	h := newRig(t, baseConfig)
	app, _ := itemRepo(t, h, itemAnchor, "", nil)
	agent := []string{"HEAVYQ_OUTPUT=agent"}
	last := func(s string) string {
		lines := strings.Split(strings.TrimSpace(s), "\n")
		return lines[len(lines)-1]
	}
	_, out, _ := h.inDir(app, agent, "item", "ls")
	if last(out) != "next: heavyq item claim --next" {
		t.Fatalf("ls:\n%s", out)
	}
	_, out, _ = h.inDir(app, agent, "item", "claim", "--next")
	if last(out) != "next: do the work, then heavyq item done web-item --run" {
		t.Fatalf("claim:\n%s", out)
	}
	_, out, _ = h.inDir(app, agent, "item", "ls")
	if last(out) != "next: heavyq item done web-item --run" {
		t.Fatalf("ls after claim:\n%s", out)
	}
	_, _, errOut := h.inDir(app, agent, "item", "done", "ui-item")
	if last(errOut) != "next: heavyq gate run web" {
		t.Fatalf("done:\n%s", errOut)
	}
}
