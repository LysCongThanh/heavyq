package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

// These tests run the real binary against a real temp repository with two
// work trees ("app", the main one, and "lane-b", linked): two lanes of one
// repository, sharing one lease store under the git common dir.

// itemAnchor is the anchor file of the repository used below. It is odd on
// purpose (trailing spaces, a tab, a CRLF line, unknown lines) so a byte-exact
// comparison means something.
const itemAnchor = "# Goal: ship login v2\n\nSome prose that stays.\n\n" +
	"- [ ] web-item: the web part  `proof: gate:web`  \n" +
	"- [ ] cmd-item: a command proof `proof: echo run >> {ROOT}/count-c; echo ok`\r\n" +
	"\t- [ ] plain-item: no proof here\n" +
	"- [x] hand-ticked: ticked by hand\n" +
	"- [ ] ui-item: needs a human `proof: gate:web`\n"

// itemRepo creates the repository with its anchor, a gate "web" and a linked
// work tree lane-b, all committed. h.cwd is the main work tree.
func itemRepo(t *testing.T, h *rig, anchor, extraToml string, files map[string]string) (app, laneB string) {
	t.Helper()
	app = gitRepo(t, h)
	toml := fmt.Sprintf("[[gate]]\nname = \"web\"\nrun = ['echo run >> %s/count-w; echo web ok']\n%s", h.root, extraToml)
	all := map[string]string{
		"heavyq.toml":       toml,
		".heavyq/anchor.md": strings.ReplaceAll(anchor, "{ROOT}", h.root),
	}
	for k, v := range files {
		all[k] = v
	}
	for name, content := range all {
		p := filepath.Join(app, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	gitIn(t, h, app, "add", ".")
	gitIn(t, h, app, "commit", "-q", "-m", "items")
	laneB = filepath.Join(h.root, "lane-b")
	gitIn(t, h, app, "worktree", "add", "-q", "-b", "lane-b", laneB)
	return app, laneB
}

// inDir runs heavyq in dir with extra environment (NAME=value), apart from
// stdout and stderr. Safe to call from several goroutines.
func (h *rig) inDir(dir string, env []string, args ...string) (rc int, stdout, stderr string) {
	cmd := exec.Command(binPath, args...)
	cmd.Dir, cmd.Env = dir, append(append([]string{}, h.env...), env...)
	var o, e bytes.Buffer
	cmd.Stdout, cmd.Stderr = &o, &e
	if err := cmd.Start(); err != nil {
		h.t.Error(err)
		return -1, "", err.Error()
	}
	return processExit(cmd.Wait()), o.String(), e.String()
}

// itemRowsJSON is `item ls --all --json` of dir, keyed by item id.
func (h *rig) itemRowsJSON(dir string) map[string]itemJSON {
	h.t.Helper()
	rc, out, errOut := h.inDir(dir, nil, "item", "ls", "--all", "--json")
	var doc itemListJSON
	if err := json.Unmarshal([]byte(out), &doc); rc != 0 || err != nil {
		h.t.Fatalf("item ls rc=%d err=%v\n%s\n%s", rc, err, out, errOut)
	}
	rows := map[string]itemJSON{}
	for _, it := range doc.Items {
		rows[it.ID] = it
	}
	return rows
}

func writeFileIn(dir, name, content string) error {
	return os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644)
}

func TestE2EItemClaimNextNeverDuplicatesAcrossWorktrees(t *testing.T) {
	h := newRig(t, baseConfig)
	var b strings.Builder
	b.WriteString("# Goal: many items\n")
	for i := 1; i <= 6; i++ {
		fmt.Fprintf(&b, "- [ ] item-%d: work %d\n", i, i)
	}
	app, laneB := itemRepo(t, h, b.String(), "", nil)

	// Eight claimers, each its own session, spread over the two work trees:
	// six items, so six get one and two find nothing.
	type result struct {
		rc         int
		item, lane string
	}
	results := make([]result, 8)
	var wg sync.WaitGroup
	for i := range results {
		wg.Add(1)
		go func() {
			defer wg.Done()
			dir := []string{app, laneB}[i%2]
			rc, out, errOut := h.inDir(dir, []string{fmt.Sprintf("HEAVYQ_SESSION=s%d", i)}, "item", "claim", "--next", "--json")
			results[i].rc = rc
			if rc == 0 {
				var c claimJSON
				if err := json.Unmarshal([]byte(out), &c); err != nil {
					t.Errorf("claim %d: %v\n%s", i, err, out)
				}
				results[i].item, results[i].lane = c.Item, c.Lane
			} else if rc != 1 || !strings.Contains(errOut, "nothing to claim") {
				t.Errorf("claim %d: rc=%d\n%s", i, rc, errOut)
			}
		}()
	}
	wg.Wait()
	var got []string
	claimedBy := map[string]int{} // which lane won how many: depends on who got there first
	for i, r := range results {
		if r.rc == 0 {
			got = append(got, r.item)
			claimedBy[r.lane]++
			if want := []string{"app", "lane-b"}[i%2]; r.lane != want {
				t.Errorf("claimer %d works in lane %s, the lease says %s", i, want, r.lane)
			}
		}
	}
	sort.Strings(got)
	want := []string{"item-1", "item-2", "item-3", "item-4", "item-5", "item-6"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("claimed %v, want each of %v exactly once", got, want)
	}
	rows := h.itemRowsJSON(app)
	lanes := map[string]int{}
	for _, r := range rows {
		if r.State != "claimed" {
			t.Fatalf("%s is %s", r.ID, r.State)
		}
		lanes[r.Lane]++
	}
	if lanes["app"] != claimedBy["app"] || lanes["lane-b"] != claimedBy["lane-b"] || len(rows) != 6 {
		t.Fatalf("leases by lane %v, claim results by lane %v", lanes, claimedBy)
	}
}

func TestE2EItemHeldReleaseForceAndLaneOverride(t *testing.T) {
	h := newRig(t, baseConfig)
	app, laneB := itemRepo(t, h, itemAnchor, "", nil)
	if rc, out, errOut := h.inDir(app, nil, "item", "claim", "web-item"); rc != 0 || !strings.Contains(out, "claimed web-item for lane app") {
		t.Fatalf("claim rc=%d\n%s%s", rc, out, errOut)
	}
	// Another lane is refused, and so is everything that would change the item.
	for _, args := range [][]string{{"claim", "web-item"}, {"release", "web-item"}, {"done", "web-item"}, {"block", "web-item", "needs: x"}} {
		rc, _, errOut := h.inDir(laneB, nil, append([]string{"item"}, args...)...)
		if rc != 1 || !strings.Contains(errOut, "held by lane") || !strings.Contains(errOut, "app") {
			t.Fatalf("%v from lane-b: rc=%d\n%s", args, rc, errOut)
		}
	}
	if rc, _, errOut := h.inDir(laneB, nil, "item", "release", "web-item", "--force"); rc != 0 {
		t.Fatalf("forced release rc=%d\n%s", rc, errOut)
	}
	// HEAVYQ_LANE names the lane when it is not the work tree's name.
	if rc, out, errOut := h.inDir(laneB, []string{"HEAVYQ_LANE=reviewer"}, "item", "claim", "web-item"); rc != 0 || !strings.Contains(out, "for lane reviewer") {
		t.Fatalf("claim as reviewer rc=%d\n%s%s", rc, out, errOut)
	}
	if rc, _, errOut := h.inDir(app, nil, "item", "claim", "no-such-item"); rc != 2 || !strings.Contains(errOut, `no item "no-such-item"`) {
		t.Fatalf("unknown item rc=%d\n%s", rc, errOut)
	}
	// Items that cannot be taken, and why.
	for id, why := range map[string]string{"hand-ticked": "already done"} {
		if rc, _, errOut := h.inDir(app, nil, "item", "claim", id); rc != 1 || !strings.Contains(errOut, why) {
			t.Fatalf("claim %s rc=%d\n%s", id, rc, errOut)
		}
	}
	rows := h.itemRowsJSON(app)
	if r := rows["hand-ticked"]; r.State != "claimed_done_unproven" || !r.Done || r.Reason == "" {
		t.Fatalf("a hand-ticked box is not proof: %+v", r)
	}
	if r := rows["web-item"]; r.State != "claimed" || r.Lane != "reviewer" {
		t.Fatalf("%+v", r)
	}
}

func TestE2EAnyRunInALaneRenewsItsLeases(t *testing.T) {
	h := newRig(t, baseConfig)
	app, laneB := itemRepo(t, h, itemAnchor, "", nil)
	for _, c := range []struct{ dir, id string }{{app, "web-item"}, {laneB, "cmd-item"}} {
		if rc, _, errOut := h.inDir(c.dir, nil, "item", "claim", c.id); rc != 0 {
			t.Fatalf("claim rc=%d\n%s", rc, errOut)
		}
	}
	leases := filepath.Join(app, ".git", "heavyq", "leases")
	soon := time.Now().Add(2 * time.Minute).UTC()
	expires := func(id string) time.Time {
		var l struct{ Expires time.Time }
		if err := json.Unmarshal([]byte(mustRead(t, filepath.Join(leases, id+".json"))), &l); err != nil {
			t.Fatal(err)
		}
		return l.Expires
	}
	for _, id := range []string{"web-item", "cmd-item"} { // age both leases: two minutes left
		p := filepath.Join(leases, id+".json")
		var m map[string]any
		if err := json.Unmarshal([]byte(mustRead(t, p)), &m); err != nil {
			t.Fatal(err)
		}
		m["expires"] = soon.Format(time.RFC3339Nano)
		b, _ := json.Marshal(m)
		if err := os.WriteFile(p, b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if rc, out := h.runIn(app, "3", "--", "true"); rc != 0 {
		t.Fatalf("run rc=%d\n%s", rc, out)
	}
	if got := expires("web-item"); time.Until(got) < 25*time.Minute {
		t.Fatalf("a run in lane app must renew its lease, expires %s", got)
	}
	if got := expires("cmd-item"); got.After(soon.Add(time.Second)) {
		t.Fatalf("a run in lane app must not renew lane-b's lease, expires %s", got)
	}
}

// runIn runs a heavyq run in dir (the rig's cwd is the main work tree).
func (h *rig) runIn(dir string, args ...string) (int, string) {
	rc, out, errOut := h.inDir(dir, nil, args...)
	return rc, out + errOut
}
