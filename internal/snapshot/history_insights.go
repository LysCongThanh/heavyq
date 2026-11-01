package snapshot

import (
	"sort"
	"time"

	"github.com/LysCongThanh/heavyq/internal/history"
)

// Insights summarises finished runs over a time window (dashboard panel and
// `heavyq history --json --insights`).
type Insights struct {
	From        time.Time  `json:"from"`
	To          time.Time  `json:"to"`
	Runs        int        `json:"runs"`        // commands that ran (cache hits excluded)
	Failed      int        `json:"failed"`      // ran, exit != 0
	GaveUp      int        `json:"gave_up"`     // left the queue without running (max-wait, remote down)
	Cancelled   int        `json:"cancelled"`   // cancelled from the dashboard / `heavyq cancel`
	Interrupted int        `json:"interrupted"` // signal (Ctrl-C, agent killed the tool call)
	Forced      int        `json:"forced"`      // admitted by the anti-starvation bypass
	CacheHits   int        `json:"cache_hits"`
	Coalesced   int        `json:"coalesced"`
	WaitP50S    float64    `json:"wait_p50_s"`
	WaitP95S    float64    `json:"wait_p95_s"`
	WaitMaxS    float64    `json:"wait_max_s"`
	RunS        float64    `json:"run_s"`   // total wall time of commands that ran
	CPUS        float64    `json:"cpu_s"`   // total CPU time (local runs)
	Lanes       []LaneStat `json:"lanes"`   // busiest first
	Buckets     []Bucket   `json:"buckets"` // equal time slices, oldest first
}

// LaneStat is one lane's share of the window.
type LaneStat struct {
	Lane     string  `json:"lane"`
	Runs     int     `json:"runs"`
	Failed   int     `json:"failed"`
	GaveUp   int     `json:"gave_up"`
	WaitP50S float64 `json:"wait_p50_s"`
	RunS     float64 `json:"run_s"`
	CPUS     float64 `json:"cpu_s"`
}

// Bucket is one time slice of the window.
type Bucket struct {
	Start    time.Time `json:"start"`
	Runs     int       `json:"runs"`
	GaveUp   int       `json:"gave_up"`
	WaitP95S float64   `json:"wait_p95_s"`
	RunS     float64   `json:"run_s"`
}

// ComputeInsights summarises records that ended in [from, to), split into n
// buckets.
func ComputeInsights(recs []history.Record, from, to time.Time, n int) Insights {
	in := Insights{From: from, To: to}
	if n < 1 {
		n = 1
	}
	span := to.Sub(from) / time.Duration(n)
	if span <= 0 {
		span = time.Second
	}
	buckets := make([]Bucket, n)
	bucketWaits := make([][]float64, n)
	for i := range buckets {
		buckets[i].Start = from.Add(time.Duration(i) * span)
	}
	lanes := map[string]*LaneStat{}
	laneWaits := map[string][]float64{}
	var waits []float64
	for _, r := range recs {
		if r.End.Before(from) || !r.End.Before(to) {
			continue
		}
		b := min(int(r.End.Sub(from)/span), n-1)
		ls := lanes[r.Lane]
		if ls == nil {
			ls = &LaneStat{Lane: r.Lane}
			lanes[r.Lane] = ls
		}
		switch r.Outcome() {
		case history.OutcomeGaveUp:
			in.GaveUp++
			ls.GaveUp++
			buckets[b].GaveUp++
			continue
		case history.OutcomeCancelled:
			in.Cancelled++
			continue
		case history.OutcomeInterrupted:
			in.Interrupted++
			if r.Machine == "" { // stopped while still waiting: not a run
				continue
			}
		}
		if r.Cache == "hit" || r.Cache == "coalesced" {
			if r.Cache == "hit" {
				in.CacheHits++
			} else {
				in.Coalesced++
			}
			continue
		}
		in.Runs++
		ls.Runs++
		buckets[b].Runs++
		if r.Exit != 0 && r.Outcome() == history.OutcomeRan {
			in.Failed++
			ls.Failed++
		}
		if r.Forced {
			in.Forced++
		}
		in.RunS += r.RunSecs
		in.CPUS += r.CPUSecs
		ls.RunS += r.RunSecs
		ls.CPUS += r.CPUSecs
		buckets[b].RunS += r.RunSecs
		waits = append(waits, r.WaitSecs)
		laneWaits[r.Lane] = append(laneWaits[r.Lane], r.WaitSecs)
		bucketWaits[b] = append(bucketWaits[b], r.WaitSecs)
	}
	in.WaitP50S, in.WaitP95S, in.WaitMaxS = percentile(waits, 50), percentile(waits, 95), percentile(waits, 100)
	for i := range buckets {
		buckets[i].WaitP95S = percentile(bucketWaits[i], 95)
	}
	in.Buckets = buckets
	for name, ls := range lanes {
		ls.WaitP50S = percentile(laneWaits[name], 50)
		in.Lanes = append(in.Lanes, *ls)
	}
	sort.Slice(in.Lanes, func(i, j int) bool {
		if in.Lanes[i].RunS != in.Lanes[j].RunS {
			return in.Lanes[i].RunS > in.Lanes[j].RunS
		}
		return in.Lanes[i].Lane < in.Lanes[j].Lane
	})
	return in
}

// percentile returns the p-th percentile (nearest rank) of v; 0 when empty.
func percentile(v []float64, p float64) float64 {
	if len(v) == 0 {
		return 0
	}
	s := append([]float64(nil), v...)
	sort.Float64s(s)
	rank := int(p/100*float64(len(s))+0.999999) - 1
	return s[max(0, min(rank, len(s)-1))]
}
