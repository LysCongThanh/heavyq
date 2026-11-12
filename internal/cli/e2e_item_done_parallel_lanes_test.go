package cli

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// #119: `item done` in parallel work trees must not leave a checkbox edit in the
// anchor file, because two lanes ticking adjacent lines merge as a conflict.
// These tests run the real binary against a real temp repository with real linked
// work trees and real `git merge`s.

// parallelAnchor has adjacent items, the worst case for a line-based merge.
const parallelAnchor = "# Goal: parallel lanes\n\n" +
	"- [ ] a: first `proof: true`\n" +
	"- [ ] b: second `proof: true`\n" +
	"- [ ] c: third `proof: true`\n" +
	"- [ ] d: fourth `proof: true`\n"

// addLane adds a linked work tree (and branch) named lane next to the repository.
func addLane(t *testing.T, h *rig, app, lane string) string {
	t.Helper()
	dir := filepath.Join(h.root, lane)
	gitIn(t, h, app, "worktree", "add", "-q", "-b", lane, dir)
	return dir
}

// gitOut is `git -C dir args...` with its combined output and error, for the
// calls that may fail (a merge that conflicts).
func gitOut(h *rig, dir string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = h.env
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// laneFinishesItem is one agent's session in its own work tree: commit its work,
// claim an item, prove it and finish it. Then it commits everything that is
// left, as an agent that does not know about the anchor does.
func laneFinishesItem(t *testing.T, h *rig, dir, id string) {
	t.Helper()
	if err := writeFileIn(dir, id+".txt", "work for "+id+"\n"); err != nil {
		t.Fatal(err)
	}
	gitIn(t, h, dir, "add", ".")
	gitIn(t, h, dir, "commit", "-q", "-m", "work "+id)
	if rc, _, errOut := h.inDir(dir, nil, "item", "claim", id); rc != 0 {
		t.Fatalf("claim %s rc=%d\n%s", id, rc, errOut)
	}
	if rc, out, errOut := h.inDir(dir, nil, "item", "done", id, "--run"); rc != 0 {
		t.Fatalf("done %s rc=%d\n%s%s", id, rc, out, errOut)
	}
	if st, _ := gitOut(h, dir, "status", "--porcelain"); strings.TrimSpace(st) != "" {
		t.Errorf("after `item done` the work tree of %s has something to commit:\n%s", id, st)
	}
	gitIn(t, h, dir, "add", "-A")
	gitIn(t, h, dir, "commit", "-q", "--allow-empty", "-m", "finish "+id)
}

func TestE2EItemDoneInParallelWorktreesMergesCleanlyAndSyncTicksOnMain(t *testing.T) {
	h := newRig(t, baseConfig)
	app, laneB := itemRepo(t, h, parallelAnchor, "", nil)
	laneC := addLane(t, h, app, "lane-c")
	anchorPath := filepath.Join(app, ".heavyq", "anchor.md")
	orig := mustRead(t, anchorPath)

	laneFinishesItem(t, h, laneB, "a")
	laneFinishesItem(t, h, laneC, "b") // the line right under a's
	for _, lane := range []string{laneB, laneC} {
		if got := mustRead(t, filepath.Join(lane, ".heavyq", "anchor.md")); got != orig {
			t.Errorf("done must leave the anchor of %s alone, it is now:\n%s", lane, got)
		}
	}

	// The lead lands both lanes on main: no conflict, however adjacent the items are.
	for _, branch := range []string{"lane-b", "lane-c"} {
		if out, err := gitOut(h, app, "merge", "--no-edit", branch); err != nil {
			t.Fatalf("merging %s: %v\n%s", branch, err, out)
		}
	}
	if got := mustRead(t, anchorPath); got != orig {
		t.Fatalf("nothing has ticked the anchor yet:\n%s", got)
	}

	// Their status comes from the events log, which every work tree shares.
	rows := h.itemRowsJSON(app)
	for _, id := range []string{"a", "b"} {
		if r := rows[id]; !r.Done || r.Checked || r.Lane != "" {
			t.Errorf("item %s finished by a lane, seen from main: %+v", id, r)
		}
	}
	for _, id := range []string{"c", "d"} {
		if r := rows[id]; r.Done || r.State != "open" {
			t.Errorf("item %s was not touched: %+v", id, r)
		}
	}
	if rc, out, _ := h.inDir(app, nil, "item", "claim", "--next", "--json"); rc != 0 || !strings.Contains(out, `"item": "c"`) {
		t.Fatalf("--next must skip the items finished in the lanes: rc=%d\n%s", rc, out)
	}

	// `item sync` is the one explicit place where the ticks reach the file.
	rc, out, errOut := h.inDir(app, nil, "item", "sync")
	if rc != 0 || !strings.Contains(out, "synced a, b") {
		t.Fatalf("sync rc=%d\n%s%s", rc, out, errOut)
	}
	want := strings.Replace(strings.Replace(orig, "- [ ] a:", "- [x] a:", 1), "- [ ] b:", "- [x] b:", 1)
	if got := mustRead(t, anchorPath); got != want {
		t.Fatalf("sync ticks exactly the two finished items.\ngot:  %q\nwant: %q", got, want)
	}
	if rc, out, _ := h.inDir(app, nil, "item", "sync"); rc != 0 || !strings.Contains(out, "nothing to sync") || mustRead(t, anchorPath) != want {
		t.Fatalf("a second sync changes nothing: rc=%d\n%s", rc, out)
	}
	if r := h.itemRowsJSON(app)["a"]; !r.Done || !r.Checked {
		t.Fatalf("after sync the box is ticked: %+v", r)
	}
	gitIn(t, h, app, "commit", "-q", "-am", "sync items")
	if st, _ := gitOut(h, app, "status", "--porcelain"); strings.TrimSpace(st) != "" {
		t.Fatalf("one commit on main carries the ticks:\n%s", st)
	}
}

func TestE2EItemClaimByNameReopensAnItemDoneOnlyInTheEventsLog(t *testing.T) {
	h := newRig(t, baseConfig)
	app, laneB := itemRepo(t, h, parallelAnchor, "", nil)
	laneFinishesItem(t, h, laneB, "a")

	if r := h.itemRowsJSON(app)["a"]; !r.Done || r.Checked {
		t.Fatalf("done in the events log only: %+v", r)
	}
	// A box that was ticked has to be unticked by hand to work on the item again (hand-ticked
	// items are refused, see e2e_item_test.go); an item done only in the log is answered by a claim.
	if rc, out, errOut := h.inDir(app, nil, "item", "claim", "a"); rc != 0 {
		t.Fatalf("claim a rc=%d\n%s%s", rc, out, errOut)
	}
	if r := h.itemRowsJSON(app)["a"]; r.Done || r.State != "claimed" {
		t.Fatalf("a claim answers the done mark: %+v", r)
	}
	orig := mustRead(t, filepath.Join(app, ".heavyq", "anchor.md"))
	if rc, out, _ := h.inDir(app, nil, "item", "sync"); rc != 0 || !strings.Contains(out, "nothing to sync") || mustRead(t, filepath.Join(app, ".heavyq", "anchor.md")) != orig {
		t.Fatalf("a reopened item is not ticked by sync: rc=%d\n%s", rc, out)
	}
}
