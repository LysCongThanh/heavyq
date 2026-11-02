package flow

import (
	"encoding/json"
	"math"
	"testing"
	"time"
)

func fleetOf(lead float64, by map[State]float64) Fleet { return Fleet{LeadS: lead, ByStateS: by} }

func TestConstraintMustExceedItsOwnBaseline(t *testing.T) {
	f := fleetOf(100, map[State]float64{Working: 50, CIWait: 40, Queued: 10})
	if c := constraintOf(f, nil); !c.Significant {
		t.Fatalf("no history: the threshold alone decides: %+v", c)
	}
	normal := &Baseline{Shares: map[State]float64{CIWait: 0.45}}
	if c := constraintOf(f, normal); c.State != CIWait || c.Significant {
		t.Errorf("40%% against a normal 45%% is not news: %+v", c)
	}
	rising := &Baseline{Shares: map[State]float64{CIWait: 0.2}}
	if c := constraintOf(f, rising); !c.Significant {
		t.Errorf("40%% against a normal 20%% is news: %+v", c)
	}
	equal := &Baseline{Shares: map[State]float64{CIWait: 0.40}}
	if c := constraintOf(f, equal); c.Significant {
		t.Errorf("share must be above its baseline, not equal: %+v", c)
	}
	low := fleetOf(100, map[State]float64{Working: 80, CIWait: 20})
	if c := constraintOf(low, &Baseline{}); c.Significant {
		t.Errorf("below 30%% is never news, whatever the baseline: %+v", c)
	}
}

func TestNextBaselineIsAnEWMAThatMovesOncePerStep(t *testing.T) {
	f := fleetOf(100, map[State]float64{Working: 50, CIWait: 40, Queued: 10})
	b1 := NextBaseline(nil, f, at(0))
	if got := b1.Of(CIWait); math.Abs(got-0.08) > 1e-9 {
		t.Fatalf("first fold from an empty baseline is alpha*share = 0.08, got %v", got)
	}
	if b1.Of(Working) != 0 {
		t.Error("working is the work, not a wait: never in the baseline")
	}
	if b := NextBaseline(b1, f, at(10)); b != b1 {
		t.Error("less than a step later the baseline must not move")
	}
	b2 := NextBaseline(b1, f, at(0).Add(BaselineStep))
	if want := BaselineAlpha*0.4 + (1-BaselineAlpha)*0.08; math.Abs(b2.Of(CIWait)-want) > 1e-9 {
		t.Errorf("second fold: %v, want %v", b2.Of(CIWait), want)
	}
	// A state that stops being a wait decays; an empty window changes nothing.
	g := fleetOf(100, map[State]float64{Working: 100})
	b3 := NextBaseline(b2, g, at(0).Add(2*BaselineStep))
	if want := (1 - BaselineAlpha) * b2.Of(CIWait); math.Abs(b3.Of(CIWait)-want) > 1e-9 {
		t.Errorf("decay: %v, want %v", b3.Of(CIWait), want)
	}
	if NextBaseline(b3, Fleet{}, at(1000)) != b3 {
		t.Error("an empty window says nothing about what is normal")
	}
	// A baseline from the future (clock step) is old.
	if b := NextBaseline(&Baseline{At: at(500)}, f, at(0)); b.At.Equal(at(500)) {
		t.Error("a future baseline must be replaced")
	}
}

func TestBuildCarriesTheBaselineAndTheRunThroughTheSnapshot(t *testing.T) {
	end := t0.Add(6 * time.Hour)
	in := Input{Now: at(60), Baseline: &Baseline{At: at(0), Shares: map[State]float64{Queued: 0.9}},
		Run: &RunInfo{ID: "r1", Started: t0, ExpectedEnd: &end, Goal: "g", Items: 3, Parallel: 2}}
	snap := Build(in)
	if snap.Baseline != nil && snap.Baseline.Of(Queued) != 0.9 {
		t.Errorf("an empty window leaves the baseline as it was: %+v", snap.Baseline)
	}
	b, err := json.Marshal(snap)
	if err != nil {
		t.Fatal(err)
	}
	var back Snapshot
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	r := back.RunInfo()
	if r == nil || r.ID != "r1" || r.Items != 3 || r.ExpectedEnd == nil || !r.ExpectedEnd.Equal(end) {
		t.Errorf("run round trip: %+v", r)
	}
	if none := Build(Input{Now: at(1)}); none.RunInfo() != nil || string(none.Run) != "null" {
		t.Errorf("no run is null: %s", none.Run)
	}
}

func TestRunInfoToleratesUnknownShapes(t *testing.T) {
	odd := Snapshot{Run: json.RawMessage(`{"id":7}`)}
	if odd.RunInfo() != nil {
		t.Error("a run of another shape reads as no run")
	}
	ok := Snapshot{Run: json.RawMessage(`{"id":"r1","started":"2026-11-02T10:00:00Z","expected_end":null,"goal":"g","items":8,"parallel":3,"future":true}`)}
	if r := ok.RunInfo(); r == nil || r.ID != "r1" || r.ExpectedEnd != nil || r.Items != 8 {
		t.Errorf("%+v", r)
	}
}

func TestConflictHotspotsRankFilesByConflictingMerges(t *testing.T) {
	c := func(files ...string) External {
		return External{Item: "x", Span: Span{State: ConflictRework}, Files: files}
	}
	got := ConflictHotspots([]External{c("CHANGELOG.md", "usage.go"), c("CHANGELOG.md"), c("CHANGELOG.md", "table.md"),
		{Item: "x", Span: Span{State: CIWait}, Files: []string{"ignored"}}})
	if len(got) != 3 || got[0].File != "CHANGELOG.md" || got[0].Count != 3 || got[0].Share != 1 || got[1].File != "table.md" {
		t.Errorf("%+v", got)
	}
	if len(ConflictHotspots(nil)) != 0 {
		t.Error("no conflicts, no hotspots")
	}
}
