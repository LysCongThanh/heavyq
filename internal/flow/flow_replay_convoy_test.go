package flow_test

import (
	"sort"
	"testing"
	"time"

	"github.com/LysCongThanh/heavyq/internal/flow"
	"github.com/LysCongThanh/heavyq/internal/flow/collect"
	"github.com/LysCongThanh/heavyq/internal/history"
	"github.com/LysCongThanh/heavyq/internal/itemstatus"
	"github.com/LysCongThanh/heavyq/internal/lease"
)

// inputAt is everything heavyq would have known at now: the ledger and the
// lease log up to now, the items as they stood, and what the git and gh
// collectors read at that instant.
func inputAt(t *testing.T, c convoy, repo string, now time.Time) flow.Input {
	t.Helper()
	in := flow.Input{Now: now, Repo: repo}
	var ids []string
	for _, p := range c.PRs {
		if p.Start.After(now) {
			continue
		}
		ids = append(ids, p.ID)
		in.Leases = append(in.Leases, lease.Event{At: p.Start, Op: lease.OpClaim, Item: p.ID, Lane: p.Lane, Session: p.Session})
		rec := func(end time.Time, run float64, gate string) {
			if !end.After(now) {
				in.Records = append(in.Records, history.Record{End: end, Lane: p.Lane, Session: p.Session, Item: p.ID,
					RunSecs: run, Cmd: "go test ./...", Gate: gate})
			}
		}
		rec(p.Start.Add(5*time.Minute), 120, "")
		for _, end := range p.ProofEnds {
			rec(end, 240, "fast")
		}
		fact := flow.ItemFact{Gate: "fast", Status: itemstatus.Status{ID: p.ID, State: itemstatus.Claimed, Lane: p.Lane, Session: p.Session, Expires: now.Add(time.Hour)}}
		if !p.Land.After(now) {
			fact.Status = itemstatus.Status{ID: p.ID, State: itemstatus.Proven, Lane: p.Lane, Session: p.Session, Commit: "abc1234", At: p.Land, Done: true}
			in.Leases = append(in.Leases, lease.Event{At: p.Land, Op: lease.OpDone, Item: p.ID, Lane: p.Lane, Session: p.Session, Commit: "abc1234"})
		}
		in.Items = append(in.Items, fact)
	}
	sort.SliceStable(in.Records, func(i, j int) bool { return in.Records[i].End.Before(in.Records[j].End) })
	ext, err := collect.All(collect.Options{Items: ids, GH: convoyGH(c, now), Now: func() time.Time { return now }})(
		repo, now.Add(-flow.DefaultWindow-time.Hour), now)
	if err != nil {
		t.Fatalf("collectors at %v: %v", now.Format("15:04"), err)
	}
	in.External = ext
	return in
}

// TestReplaySerialCIConvoy is the proof of the flow-collectors item: replaying
// a serial-CI convoy (four parallel pull requests, every one merging main
// again and again, 8 to 10 minutes of CI per rerun, the same three files
// conflicting every time) the constraint finder must name ci_wait or
// conflict_rework as significant, and name it while the convoy is still going.
func TestReplaySerialCIConvoy(t *testing.T) {
	c := planConvoy()
	repo := buildConvoyRepo(t, c)
	end := c.PRs[len(c.PRs)-1].Land.Add(5 * time.Minute)

	// The fixture is what it claims to be.
	if len(c.PRs) < 3 || len(c.PRs) > 5 {
		t.Fatalf("%d parallel pull requests, want 3 to 5", len(c.PRs))
	}
	for _, p := range c.PRs {
		if len(p.Merges) < 2 {
			t.Errorf("%s merged main %d times, want at least 2", p.ID, len(p.Merges))
		}
		for _, r := range p.Runs {
			if d := r.End.Sub(r.Start); !r.Cancelled && (d < 8*time.Minute || d > 10*time.Minute) {
				t.Errorf("%s: a CI rerun of %v, want 8 to 10 minutes", p.ID, d)
			}
		}
	}

	// Replay: every quarter of an hour, with what was known then and the baseline
	// the finder had learned by then.
	var base *flow.Baseline
	var first time.Time
	var last flow.Snapshot
	for now := minute(20); !now.After(end); now = now.Add(15 * time.Minute) {
		in := inputAt(t, c, repo, now)
		in.Baseline = base
		last = flow.Build(in)
		base = last.Baseline
		cs := last.Constraint
		if first.IsZero() && cs.Significant && (cs.State == flow.CIWait || cs.State == flow.ConflictRework) {
			first = now
		}
	}
	if first.IsZero() {
		t.Fatalf("the constraint was never ci_wait or conflict_rework and significant; at the end: %+v fleet %+v", last.Constraint, last.Fleet.ByStateS)
	}
	// The owner noticed when the last pull request landed; the finder must have
	// named it at least an hour earlier.
	if owner := c.PRs[len(c.PRs)-1].Land; owner.Sub(first) < time.Hour {
		t.Errorf("named at %s, only %v before the last pull request landed (%s): not early enough", first.Format("15:04"), owner.Sub(first), owner.Format("15:04"))
	}
	t.Logf("constraint first named at %s; at the end %+v, shares %v", first.Format("15:04"), last.Constraint, last.Fleet.ByStateS)

	// The end state, with the whole window in view, still names it, from both sources.
	final := inputAt(t, c, repo, minute(100))
	snap := flow.Build(final)
	if cs := snap.Constraint; !cs.Significant || cs.Share < flow.SignificantShare || (cs.State != flow.CIWait && cs.State != flow.ConflictRework) {
		t.Errorf("mid-convoy constraint: %+v (by state %v)", cs, snap.Fleet.ByStateS)
	}
	for _, st := range []flow.State{flow.CIWait, flow.ConflictRework} {
		if snap.Fleet.ByStateS[st] <= 0 {
			t.Errorf("no %s in the fleet's lead time: the collector contributed nothing", st)
		}
	}

	// Control: without the collectors the same facts name neither state, so the
	// naming above is the collectors' doing and not an accident of the fixture.
	blind := final
	blind.External = nil
	if f := flow.Build(blind).Fleet.ByStateS; f[flow.CIWait] != 0 || f[flow.ConflictRework] != 0 || f[flow.MergeWait] != 0 {
		t.Errorf("without collectors there must be no gh or git states: %v", f)
	}

	// git: every pull request has at least two conflicting merges of main, and
	// the conflicts sit in the three hot files.
	perItem := map[string]int{}
	for _, e := range final.External {
		if e.Span.State == flow.ConflictRework {
			perItem[e.Item]++
		}
	}
	for _, p := range c.PRs {
		if perItem[p.ID] < 2 {
			t.Errorf("%s: %d conflict spans from git, want at least 2", p.ID, perItem[p.ID])
		}
	}
	hs := flow.ConflictHotspots(final.External)
	if len(hs) != len(hotFiles) {
		t.Fatalf("conflicts in %d files, want exactly the %d hot ones: %+v", len(hs), len(hotFiles), hs)
	}
	for _, h := range hs {
		if h.Share < 0.9 || !contains(hotFiles, h.File) {
			t.Errorf("hotspot %+v: want a hot file in at least 90%% of the conflicts", h)
		}
	}
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}
