package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

// proofsAnchor: four items, two on the gate "web" and two on a command each. None of them is ever claimed.
const proofsAnchor = "# Goal: every proof counts\n\n" +
	"- [ ] gate-a: first on the web gate  `proof: gate:web`\n" +
	"- [ ] gate-b: second on the web gate  `proof: gate:web`\n" +
	"- [ ] cmd-a: a command proof `proof: echo a-ok`\n" +
	"- [ ] cmd-b: a command proof `proof: echo b-ok`\n"

// The morning recap after a night of proofs by one lead that never claimed anything (#141): the code moved on after
// the proofs, so the items read stale. The recap and `flow show` must say "done, stale" (not "claimed, unproven",
// not "open") with the proof run as the action, and the throughput counts each proved item, not only
// claim-to-proof pairs.
func TestE2ERecapCountsEveryProof(t *testing.T) {
	h := newRig(t, baseConfig)
	h.env = append(h.env, "HEAVYQ_NOTIFY=off", "LANG=en_US.UTF-8", "NO_COLOR=1")
	env := append([]string{}, flowOn...)
	app, _ := itemRepo(t, h, proofsAnchor, "", nil)

	// Proved without a claim: the gate for gate-a and gate-b, the done events for gate-a, cmd-a and cmd-b.
	for _, step := range [][]string{{"gate", "run", "web"}, {"item", "done", "gate-a"}, {"item", "done", "cmd-a", "--run"}, {"item", "done", "cmd-b", "--run"}} {
		if rc, out, errOut := h.inDir(app, env, step...); rc != 0 {
			t.Fatalf("%v: rc=%d\n%s%s", step, rc, out, errOut)
		}
	}
	// The code changes: every proof is now of older code.
	if err := os.WriteFile(filepath.Join(app, "feature.txt"), []byte("new code\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, h, app, "add", "-A")
	gitIn(t, h, app, "commit", "-q", "-m", "feature")
	rows := h.itemRowsJSON(app)
	for _, id := range []string{"gate-a", "cmd-a", "cmd-b"} {
		if r := rows[id]; r.State != "stale" || !r.Done {
			t.Fatalf("%s should be done and stale: %+v", id, r)
		}
	}
	if r := rows["gate-b"]; r.State != "stale" || r.Done {
		t.Fatalf("gate-b was proved but never ticked: %+v", r)
	}

	if rc, out, errOut := h.inDir(app, env, "flow", "refresh"); rc != 0 {
		t.Fatalf("flow refresh rc=%d\n%s%s", rc, out, errOut)
	}
	var raw struct {
		Items      map[string]int `json:"items"`
		Throughput struct {
			N         int        `json:"n"`
			P50       float64    `json:"p50_claim_to_proof_s"`
			LastProof *time.Time `json:"last_proof"`
		} `json:"throughput"`
	}
	out := h.mustRun(app, 0, "flow", "show", "--json")
	if err := json.Unmarshal([]byte(out), &raw); err != nil {
		t.Fatalf("flow show --json: %v\n%s", err, out)
	}
	for k, want := range map[string]int{"total": 4, "proven": 0, "claimed_unproven": 0, "done_stale": 3, "open": 1} {
		if raw.Items[k] != want {
			t.Errorf("flow items.%s = %d, want %d\n%s", k, raw.Items[k], want, out)
		}
	}
	if raw.Throughput.N != 4 || raw.Throughput.LastProof == nil || raw.Throughput.P50 != 0 {
		t.Errorf("four items passed their proof in the window and none was claimed: %+v", raw.Throughput)
	}
	if text := h.mustRun(app, 0, "flow", "show"); !strings.Contains(text, "3 done-stale") || !strings.Contains(text, "1 open") || !strings.Contains(text, "4 proven in the last") {
		t.Errorf("flow show text:\n%s", text)
	}

	var r struct {
		DoneStale []struct{ ID, Action string } `json:"done_stale"`
		Claimed   []json.RawMessage             `json:"claimed_unproven"`
	}
	out = h.mustRun(app, 0, "recap", "--json", "--since", "1h")
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		t.Fatalf("recap --json: %v\n%s", err, out)
	}
	var got []string
	for _, l := range r.DoneStale {
		got = append(got, l.ID)
		if l.Action != "heavyq item done "+l.ID+" --run" {
			t.Errorf("%s: action %q", l.ID, l.Action)
		}
	}
	sort.Strings(got)
	if strings.Join(got, ",") != "cmd-a,cmd-b,gate-a" || len(r.Claimed) != 0 {
		t.Errorf("done_stale = %v, claimed_unproven = %d\n%s", got, len(r.Claimed), out)
	}
	text := h.mustRun(app, 0, "recap")
	if !strings.Contains(text, "done · stale (3)") || strings.Contains(text, "claimed, unproven") {
		t.Errorf("recap:\n%s", text)
	}
}
