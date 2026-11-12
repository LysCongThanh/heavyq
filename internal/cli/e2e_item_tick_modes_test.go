package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// `[items] tick` says whether `item done` ticks the box in the anchor file (#119).
// "auto" (the default) ticks unless the repository has more than one work tree;
// "always" and "never" do not look. Whatever the mode, the done event is recorded
// and the item reads done.

// singleTreeRepo is itemRepo without its linked work tree: a repository with one work tree.
func singleTreeRepo(t *testing.T, h *rig, extraToml string, files map[string]string) string {
	t.Helper()
	app, laneB := itemRepo(t, h, parallelAnchor, extraToml, files)
	gitIn(t, h, app, "worktree", "remove", laneB)
	return app
}

func TestE2EItemDoneTickModes(t *testing.T) {
	for _, c := range []struct {
		name, toml string
		linked     bool // the repository has a second work tree
		ticks      bool
	}{
		{"auto in a repository of one work tree ticks", "", false, true},
		{"auto with a linked work tree defers", "", true, false},
		{"always ticks with a linked work tree", "\n[items]\ntick = \"always\"\n", true, true},
		{"never defers in a repository of one work tree", "\n[items]\ntick = \"never\"\n", false, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			h := newRig(t, baseConfig)
			var app string
			if c.linked {
				app, _ = itemRepo(t, h, parallelAnchor, c.toml, nil)
			} else {
				app = singleTreeRepo(t, h, c.toml, nil)
			}
			anchorPath := filepath.Join(app, ".heavyq", "anchor.md")
			orig := mustRead(t, anchorPath)
			if rc, _, errOut := h.inDir(app, nil, "item", "claim", "a"); rc != 0 {
				t.Fatalf("claim rc=%d\n%s", rc, errOut)
			}
			rc, out, errOut := h.inDir(app, nil, "item", "done", "a", "--run", "--json")
			if rc != 0 || !strings.Contains(out, fmt.Sprintf(`"ticked": %v`, c.ticks)) {
				t.Fatalf("done rc=%d\n%s%s", rc, out, errOut)
			}
			got := mustRead(t, anchorPath)
			if c.ticks != (got != orig) {
				t.Fatalf("ticks=%v but the anchor is:\n%s", c.ticks, got)
			}
			if ev := mustRead(t, filepath.Join(app, ".git", "heavyq", "events.jsonl")); !strings.Contains(ev, `"op":"done"`) {
				t.Fatalf("the done event is recorded in every mode: %s", ev)
			}
			if r := h.itemRowsJSON(app)["a"]; !r.Done || r.Checked != c.ticks || r.Lane != "" {
				t.Fatalf("after done: %+v", r)
			}
		})
	}
}

func TestE2EItemSyncTicksWhatDoneLeftAndNeverPlanFiles(t *testing.T) {
	h := newRig(t, baseConfig)
	plan := "# Phase 1\n\n- [ ] Echo thing `proof: true`\n"
	app := singleTreeRepo(t, h, "\n[items]\ntick = \"never\"\n\n[anchor]\nextra = [\"plans/*/phase-*.md\"]\n",
		map[string]string{"plans/p1/phase-01.md": plan})
	anchorPath, planPath := filepath.Join(app, ".heavyq", "anchor.md"), filepath.Join(app, "plans", "p1", "phase-01.md")
	orig := mustRead(t, anchorPath)
	var planID string
	for id, r := range h.itemRowsJSON(app) {
		if r.ReadOnly {
			planID = id
		}
	}
	for _, id := range []string{"a", planID} {
		if rc, _, errOut := h.inDir(app, nil, "item", "claim", id); rc != 0 {
			t.Fatalf("claim %s rc=%d\n%s", id, rc, errOut)
		}
		if rc, out, errOut := h.inDir(app, nil, "item", "done", id, "--run"); rc != 0 {
			t.Fatalf("done %s rc=%d\n%s%s", id, rc, out, errOut)
		}
	}
	if mustRead(t, anchorPath) != orig {
		t.Fatal(`tick = "never": done leaves the box alone`)
	}
	rc, out, errOut := h.inDir(app, nil, "item", "sync", "--json")
	if rc != 0 || !strings.Contains(out, `"synced": [`) || !strings.Contains(out, `"a"`) || strings.Contains(out, "phase-01") {
		t.Fatalf("sync --json rc=%d\n%s%s", rc, out, errOut)
	}
	if got, want := mustRead(t, anchorPath), strings.Replace(orig, "- [ ] a:", "- [x] a:", 1); got != want {
		t.Fatalf("sync ticks item a only.\ngot:  %q\nwant: %q", got, want)
	}
	if mustRead(t, planPath) != plan {
		t.Fatal("a plan file is read-only: sync never rewrites it")
	}
}

func TestE2EItemClaimByNameAnswersAHandUntickedBox(t *testing.T) {
	h := newRig(t, baseConfig)
	app := singleTreeRepo(t, h, "", nil)
	anchorPath := filepath.Join(app, ".heavyq", "anchor.md")
	orig := mustRead(t, anchorPath)
	if rc, _, errOut := h.inDir(app, nil, "item", "claim", "a"); rc != 0 {
		t.Fatalf("claim rc=%d\n%s", rc, errOut)
	}
	if rc, out, errOut := h.inDir(app, nil, "item", "done", "a", "--run"); rc != 0 {
		t.Fatalf("done rc=%d\n%s%s", rc, out, errOut)
	}
	if rc, _, errOut := h.inDir(app, nil, "item", "claim", "a"); rc != 1 || !strings.Contains(errOut, "untick it") {
		t.Fatalf("a ticked box is refused with the way out: rc=%d\n%s", rc, errOut)
	}
	// The documented way back to work on a finished item whose code has since changed:
	// untick it by hand, then claim it. A done event in the log must not get in its way.
	if err := os.WriteFile(anchorPath, []byte(orig), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeFileIn(app, "more.txt", "the code moved on\n"); err != nil {
		t.Fatal(err)
	}
	gitIn(t, h, app, "add", ".")
	gitIn(t, h, app, "commit", "-q", "-m", "more work")
	if rc, _, errOut := h.inDir(app, nil, "item", "claim", "a"); rc != 0 {
		t.Fatalf("claim after the untick rc=%d\n%s", rc, errOut)
	}
	if r := h.itemRowsJSON(app)["a"]; r.Done || r.State != "claimed" {
		t.Fatalf("reopened: %+v", r)
	}
}
