package stance

import (
	"testing"

	"github.com/LysCongThanh/heavyq/internal/history"
)

func TestFirstProofAfterAndBaseline(t *testing.T) {
	// Lane history: two earlier proofs (a 3-run stretch and a 2-run one), then the stance
	// is entered at minute 100, and the lane passes at minute 112 after 3 more runs.
	recs := []history.Record{
		run(10, "go test", 1, "a"), run(15, "go test", 1, "b"), run(20, "go test", 0, "c"),
		run(30, "go test", 1, "d"), run(40, "go test", 0, "e"),
		run(101, "go test", 1, "f"), run(105, "go test", 1, "g",
			func(r *history.Record) { r.Cache = "hit" }), // a replay is not a run
		run(108, "go test", 1, "h"), run(112, "go test", 0, "i"),
	}
	p := FirstProofAfter(recs, t0.Add(100*60e9))
	if !p.Reached || p.Runs != 3 || p.Secs != 12*60 {
		t.Errorf("proof after entry: %+v, want 3 runs in 12 minutes", p)
	}
	if p := FirstProofAfter(recs[:8], t0.Add(100*60e9)); p.Reached || p.Runs != 2 {
		t.Errorf("not yet proven: %+v, want 2 failed runs", p)
	}
	if p := FirstProofAfter(nil, t0); p.Reached || p.Runs != 0 {
		t.Errorf("no runs: %+v", p)
	}

	b := LaneBaseline(recs, t0.Add(100*60e9))
	// Stretch 1: first run started at minute 9.5 (30 s run), passed at 20 -> 630 s, 3 runs.
	// Stretch 2: from the pass at 20 to the pass at 40 -> 1200 s, 2 runs. Medians: 915 s, 2.5 runs.
	if !b.Measured || b.Samples != 2 || b.MedRuns != 2.5 || b.MedSecs != 915 {
		t.Errorf("baseline: %+v", b)
	}
	if b := LaneBaseline(recs, t0); b.Measured {
		t.Errorf("nothing before the first run: %+v", b)
	}
}

func TestStatsPerStance(t *testing.T) {
	recs := []history.Record{
		run(10, "go test", 1, "a"), run(20, "go test", 0, "b"),
		run(60, "go test", 1, "c"), run(70, "go test", 0, "d"),
	}
	events := []Event{
		{At: t0.Add(55 * 60e9), Op: OpEnter, Lane: "wt", Stance: "breakthrough", Trigger: TriggerKeyword},
		{At: t0.Add(80 * 60e9), Op: OpExit, Lane: "wt", Stance: "breakthrough", Reason: ExitDecisionRecord},
		{At: t0.Add(200 * 60e9), Op: OpEnter, Lane: "wt", Stance: "breakthrough", Trigger: TriggerFailureClass},
		{At: t0.Add(210 * 60e9), Op: OpEnter, Lane: "other", Stance: "calm", Trigger: TriggerManual},
		{At: t0.Add(220 * 60e9), Op: OpExit, Lane: "other", Stance: "calm", Reason: ExitManual},
	}
	acts := Activations(events)
	if len(acts) != 3 || acts[0].Reason != ExitDecisionRecord || acts[1].Reason != "" {
		t.Fatalf("activations: %+v", acts)
	}
	got := Stats(acts, func(lane string) []history.Record {
		if lane == "wt" {
			return recs
		}
		return nil
	})
	if len(got) != 2 || got[0].Stance != "breakthrough" || got[1].Stance != "calm" {
		t.Fatalf("stats: %+v", got)
	}
	bt := got[0]
	if bt.Activations != 2 || bt.Active != 1 || bt.Proven != 1 || bt.MedProofSecs != 15*60 || bt.MedProofRuns != 2 ||
		bt.Baselined != 1 || bt.ByTrigger["keyword"] != 1 || bt.ByExit[ExitDecisionRecord] != 1 {
		t.Errorf("breakthrough: %+v", bt)
	}
	if calm := got[1]; calm.Activations != 1 || calm.Proven != 0 || calm.NoProof != 1 {
		t.Errorf("calm: %+v", calm)
	}
}
