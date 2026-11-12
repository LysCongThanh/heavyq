package recap

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/LysCongThanh/heavyq/internal/itemstatus"
)

// A ticked item whose proof passed on code that has changed since is done, and stale: it has its own bucket, and
// "claimed, unproven" keeps only what nothing ever proved. The action is one more proof run.
func TestDoneStaleIsNotClaimedUnproven(t *testing.T) {
	f := Facts{Now: now, Since: at(12, 0), Items: []Item{
		{ID: "a", State: itemstatus.Stale, Done: true, Commit: "abc1234", At: at(11, 0), Text: "Login"},
		{ID: "b", State: itemstatus.Stale, Done: true, Commit: "def5678", At: at(11, 30), Text: "Refresh"},
		{ID: "c", State: itemstatus.ClaimedDoneUnproven, Done: true, Reason: "ticked, but the item declares no proof"},
		{ID: "d", State: itemstatus.Stale, Text: "proven once, never ticked"},
	}}
	r := Build(f)
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		DoneStale       []Line `json:"done_stale"`
		ClaimedUnproven []Line `json:"claimed_unproven"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	stale := doc.DoneStale
	if len(stale) != 2 || stale[0].ID != "a" || stale[1].ID != "b" {
		t.Fatalf("done_stale = %+v", stale)
	}
	for _, l := range stale {
		if l.Action != "heavyq item done "+l.ID+" --run" {
			t.Errorf("%s: action %q, want the proof run", l.ID, l.Action)
		}
	}
	if cu := doc.ClaimedUnproven; len(cu) != 1 || cu[0].ID != "c" {
		t.Errorf("claimed_unproven = %+v: only the ticked item nothing proved", cu)
	}
	out := Render(r, RenderOptions{})
	for _, want := range []string{"\ndone · stale (2)\n", "\nclaimed, unproven (1)\n", "abc1234"} {
		if !strings.Contains(out, want) {
			t.Errorf("recap lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "\n  d  ") {
		t.Errorf("an item nobody ticked is open, not done:\n%s", out)
	}
	if ascii := Render(r, RenderOptions{ASCII: true}); !strings.Contains(ascii, "\ndone . stale (2)\n") || strings.ContainsAny(ascii, "·") {
		t.Errorf("ascii form:\n%s", ascii)
	}
}

// With only done-and-stale items left the next action is to prove them again, and a stale item nobody ticked is
// open work that --next may hand out.
func TestBestActionForDoneStale(t *testing.T) {
	f := Facts{Now: now, Since: at(12, 0), Items: []Item{{ID: "a", State: itemstatus.Stale, Done: true, Commit: "abc1234"}}}
	if r := Build(f); r.Best == nil || r.Best.Command != "heavyq item done a --run" {
		t.Errorf("best = %+v", r.Best)
	}
	f.Items = []Item{{ID: "d", State: itemstatus.Stale}}
	if r := Build(f); r.Best == nil || r.Best.Command != "heavyq item claim --next" || len(r.ClaimedUnproven) != 0 {
		t.Errorf("a stale item nobody ticked is open: best = %+v, claimed_unproven = %+v", r.Best, r.ClaimedUnproven)
	}
}
