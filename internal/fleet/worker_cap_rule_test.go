package fleet

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// capQueue is one queue of a worker cap case: its history file (nil: the queue has none), the slots its
// probed lock files show busy now on "box", and its configured heavy slots there.
type capQueue struct {
	history    []string // "<ended s ago> <run_s> <light 0|1> <machine>"
	busy       int
	configured int
}

// capFixture writes q's history as a real TSV file, reads it back the way `heavyq fleet` does (from its
// end, bounded by the window) and returns the queue input and the probed slot locks.
func capFixture(t *testing.T, name string, q capQueue, now time.Time, window time.Duration) (QueueInput, []SlotState) {
	t.Helper()
	in := QueueInput{Name: name, Kind: KindForeign, HeavySlots: map[string]int{"box": q.configured}}
	if q.history != nil {
		var b strings.Builder
		for _, l := range q.history {
			var ago, runS, light int
			var machine string
			if _, err := fmt.Sscan(l, &ago, &runS, &light, &machine); err != nil {
				t.Fatalf("%q: %v", l, err)
			}
			fmt.Fprintf(&b, "%d\t%s\tlane\t1\t%d\t5\t%d\t0\n", now.Unix()-int64(ago), machine, light, runS)
		}
		path := filepath.Join(t.TempDir(), name+".tsv")
		if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
			t.Fatal(err)
		}
		h, err := ReadHistoryTSV(HistorySource{Path: path, Columns: []string{"end_epoch", "machine", "lane", "prio", "light", "wait_s", "run_s", "exit"},
			Since: now.Add(-window)})
		if err != nil {
			t.Fatal(err)
		}
		in.Runs, in.History = h.Runs, true
	}
	var locks []SlotState
	for i := 0; i < max(q.busy, q.configured); i++ {
		locks = append(locks, SlotState{Owner: name, Name: fmt.Sprintf("s%d", i+1), Busy: i < q.busy})
	}
	return in, locks
}

// The worker cap counts, per queue, the heavy slots in use (max of busy now and the most at once in the
// window), the configured slots only when the queue has no history; every number is in the reasoning.
func TestWorkerCapCountsSlotsInUse(t *testing.T) {
	now := time.Date(2026, 11, 12, 10, 0, 0, 0, time.UTC)
	window := 24 * time.Hour
	fourAtOnce := []string{"7200 3600 0 box", "7000 3600 0 box", "6900 3600 0 box", "6800 3600 0 box", "60 30 0 box"}
	for _, tc := range []struct {
		name          string
		cores         int
		heavyq, bashq capQueue
		workers       int    // 0: no cap
		slots         int    // heavy slots counted
		why           string // a part of the reasoning
	}{
		{name: "an idle configured queue is not load (the 'cap 1' of every remote)", cores: 12,
			heavyq:  capQueue{history: []string{"600 300 0 other", "500 300 0 other"}, configured: 3},
			bashq:   capQueue{history: fourAtOnce, busy: 1, configured: 4},
			workers: 3, slots: 4, why: "heavyq 0 = max(now 0, peak 0) of 3; bashq 4 = max(now 1, peak 4) of 4"},
		{name: "busy now above the window", cores: 12,
			bashq:   capQueue{history: []string{}, busy: 3, configured: 4},
			workers: 4, slots: 3, why: "bashq 3 = max(now 3, peak 0) of 4"},
		{name: "the window above busy now", cores: 16,
			heavyq:  capQueue{history: []string{"3000 2000 0 box", "2500 2000 0 box", "200 100 0 box"}, configured: 2},
			workers: 8, slots: 2, why: "heavyq 2 = max(now 0, peak 2) of 2"},
		{name: "a slot handed over is not two in use", cores: 12,
			bashq:   capQueue{history: []string{"200 100 0 box", "100 100 0 box", "0 100 0 box"}},
			workers: 12, slots: 1, why: "bashq 1 = max(now 0, peak 1))"},
		{name: "light runs have slots of their own", cores: 12,
			bashq:   capQueue{history: []string{"300 300 1 box", "300 300 1 box", "300 300 1 box", "300 300 0 box"}, configured: 4},
			workers: 12, slots: 1, why: "bashq 1 = max(now 0, peak 1) of 4"},
		{name: "runs older than the window are not read", cores: 12,
			bashq:   capQueue{history: append([]string{"200000 3600 0 box", "200000 3600 0 box", "200000 3600 0 box"}, "60 30 0 box"), configured: 4},
			workers: 12, slots: 1, why: "peak 1"},
		{name: "never above the configured heavy slots (busy locks count light slots too)", cores: 12,
			bashq:   capQueue{history: fourAtOnce, busy: 6, configured: 4},
			workers: 3, slots: 4, why: "bashq 4 = max(now 6, peak 4) of 4"},
		{name: "no history: the configured slots", cores: 12,
			bashq:   capQueue{busy: 1, configured: 4},
			workers: 3, slots: 4, why: "bashq 4 configured, no history"},
		{name: "no history, nothing configured: busy now", cores: 12,
			bashq:   capQueue{busy: 2},
			workers: 6, slots: 2, why: "bashq 2 busy now, no history"},
		{name: "nothing in use anywhere: the run being started is the one", cores: 12,
			heavyq:  capQueue{history: []string{}, configured: 2},
			workers: 12, slots: 0, why: "12 cores / 1 run (no heavy slot in use now or in the window)"},
		{name: "no queue knows the machine: nothing to suggest", cores: 12,
			heavyq: capQueue{history: []string{"60 30 0 other"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			hq, hl := capFixture(t, OwnerHeavyq, tc.heavyq, now, window)
			bq, bl := capFixture(t, "bashq", tc.bashq, now, window)
			hq.Kind = KindHeavyq
			r := Assemble(Inputs{Now: now, Since: now.Add(-window), Queues: []QueueInput{hq, bq}, Machines: []MachineInput{{
				Name: "box", Role: RoleRemote, HeavyqSlots: tc.heavyq.configured, HeavyqTotal: tc.heavyq.configured,
				Probe: &Probe{Cores: tc.cores, Slots: append(hl, bl...)}}}})
			c := r.Machines[0].Cap
			if tc.workers == 0 {
				if c != nil {
					t.Fatalf("no cap expected: %+v", c)
				}
				return
			}
			if c == nil || c.Workers != tc.workers || c.HeavySlots != tc.slots || c.Cores != tc.cores || !strings.Contains(c.Why, tc.why) {
				t.Fatalf("cap %+v\nwant %d workers, %d slots, why containing %q", c, tc.workers, tc.slots, tc.why)
			}
			if !strings.HasSuffix(c.Why, fmt.Sprintf("= %d workers per run", tc.workers)) {
				t.Errorf("the arithmetic ends in the result: %q", c.Why)
			}
		})
	}
}

func TestPeakConcurrency(t *testing.T) {
	at := func(end, runS float64) Run { return Run{End: time.Unix(int64(1e9+end), 0), RunS: runS} }
	for _, tc := range []struct {
		name string
		runs []Run
		want int
	}{
		{"none", nil, 0},
		{"one", []Run{at(100, 50)}, 1},
		{"nested", []Run{at(100, 100), at(80, 10), at(90, 30)}, 3},
		{"back to back", []Run{at(100, 50), at(150, 50), at(200, 50)}, 1},
		{"no run_s still counts", []Run{at(100, 0), at(100, 0)}, 2},
		{"two waves", []Run{at(100, 50), at(110, 50), at(300, 10), at(301, 10), at(302, 10)}, 3},
	} {
		if got := PeakConcurrency(tc.runs); got != tc.want {
			t.Errorf("%s: %d, want %d", tc.name, got, tc.want)
		}
	}
}
