package mentor

import (
	"testing"

	"github.com/LysCongThanh/heavyq/internal/history"
)

func TestResolveOutcome(t *testing.T) {
	ev := Entry{Event: "id01", RunID: "A", Tree: "tree-A"}
	cases := []struct {
		name string
		lane []history.Record
		want string
	}{
		{"nothing followed yet", []history.Record{run(1, "A", 1)}, OutcomePending},
		{"the next run is another command", []history.Record{run(1, "A", 1), run(2, "B", 0)}, OutcomeDiffered},
		{"the next run is the same command on an edited tree", []history.Record{run(1, "A", 1), run(2, "A2", 0)}, OutcomeDiffered},
		{"2nd run repeats, 3rd differs: still within 3", []history.Record{run(1, "A", 1), run(2, "A", 1), run(3, "C", 0)}, OutcomeDiffered},
		{"three identical runs follow", []history.Record{run(1, "A", 1), run(2, "A", 1), run(3, "A", 1), run(4, "A", 1)}, OutcomeSame},
		{"a change in the 4th run is too late", []history.Record{run(1, "A", 1), run(2, "A", 1), run(3, "A", 1), run(4, "A", 1), run(5, "B", 0)}, OutcomeSame},
		{"two identical so far", []history.Record{run(1, "A", 1), run(2, "A", 1), run(3, "A", 1)}, OutcomePending},
		{"a run that never ran does not count", []history.Record{run(1, "A", 1), run(2, "B", 0, gaveUp)}, OutcomePending},
		{"a cache replay of the same run repeats it", []history.Record{run(1, "A", 1), run(2, "A", 1, replay), run(3, "A", 1), run(4, "A", 1)}, OutcomeSame},
		{"the event's record aged out of the lane", []history.Record{run(7, "A", 1), run(8, "B", 0)}, OutcomeUnknown},
		{"no records", nil, OutcomeUnknown},
	}
	for _, c := range cases {
		if got := ResolveOutcome(ev, c.lane); got != c.want {
			t.Errorf("%s: got %s, want %s", c.name, got, c.want)
		}
	}
}

func TestResolveOutcomeWithoutRunIDComparesTrees(t *testing.T) {
	noID := func(tree string) func(*history.Record) {
		return func(r *history.Record) { r.RunID, r.Tree = "", tree }
	}
	ev := Entry{Event: "id01", Tree: "T1"} // run_id empty
	same := []history.Record{run(1, "", 1, noID("T1")), run(2, "", 1, noID("T1"))}
	if got := ResolveOutcome(ev, same); got != OutcomePending {
		t.Fatalf("same tree: %s", got)
	}
	edited := []history.Record{run(1, "", 1, noID("T1")), run(2, "", 1, noID("T2"))}
	if got := ResolveOutcome(ev, edited); got != OutcomeDiffered {
		t.Fatalf("edited tree: %s", got)
	}
}
