package fleet

import (
	"fmt"
	"slices"
	"strings"
	"time"
)

// QueueSlots is what the worker cap knows of one queue on one machine.
type QueueSlots struct {
	Owner      string
	BusyNow    int  // slots busy now: probed slot locks, else running tickets that say they run there
	WindowPeak int  // the most heavy runs at once in the window, from the queue's history
	History    bool // the queue's history was read: WindowPeak is data, even when it is 0
	Configured int  // heavy slots configured there (0: unknown)
}

// Cap bases: how a queue's slots in use were counted.
const (
	CapBasisUsed       = "used"       // max(busy now, window peak), at most the configured heavy slots
	CapBasisConfigured = "configured" // no history: the configured heavy slots (or busy now, if more)
	CapBasisBusy       = "busy_now"   // no history and nothing configured: the slots busy now
)

// CapQueue is one queue's term of the worker cap, every number shown.
type CapQueue struct {
	Owner      string `json:"owner"`
	BusyNow    int    `json:"busy_now"`
	WindowPeak int    `json:"window_peak"` // 0 also when there is no history (see basis)
	Configured int    `json:"configured"`  // 0 when unknown
	Counted    int    `json:"counted"`
	Basis      string `json:"basis"` // "used", "configured" or "busy_now"
}

// WorkerCap is the suggested number of workers (test runner processes or threads) per heavy run.
type WorkerCap struct {
	Workers    int        `json:"workers"`
	Cores      int        `json:"cores"`
	HeavySlots int        `json:"heavy_slots"` // the heavy slots counted as in use, every queue together
	Queues     []CapQueue `json:"queues"`
	Why        string     `json:"why"`
}

// count is a queue's heavy slots in use on the machine. With a history it is the most that were in use at
// once, now or in the window (a configured slot that ran nothing is not load); never more than the
// configured heavy slots when those are known, since busy-now locks may include light slots. Without a
// history only the configured slots say what the queue may do there.
func (q QueueSlots) count() CapQueue {
	c := CapQueue{Owner: q.Owner, BusyNow: q.BusyNow, WindowPeak: q.WindowPeak, Configured: q.Configured}
	switch {
	case q.History:
		c.Basis, c.Counted = CapBasisUsed, max(q.BusyNow, q.WindowPeak)
		if q.Configured > 0 {
			c.Counted = min(c.Counted, q.Configured)
		}
	case q.Configured > 0:
		c.Basis, c.Counted = CapBasisConfigured, max(q.BusyNow, q.Configured)
	default:
		c.Basis, c.Counted = CapBasisBusy, q.BusyNow
	}
	return c
}

func (c CapQueue) text() string {
	switch c.Basis {
	case CapBasisConfigured:
		return fmt.Sprintf("%s %d configured, no history", c.Owner, c.Counted)
	case CapBasisBusy:
		return fmt.Sprintf("%s %d busy now, no history", c.Owner, c.Counted)
	}
	of := ""
	if c.Configured > 0 {
		of = fmt.Sprintf(" of %d", c.Configured)
	}
	return fmt.Sprintf("%s %d = max(now %d, peak %d)%s", c.Owner, c.Counted, c.BusyNow, c.WindowPeak, of)
}

// SuggestCap divides the cores among the heavy slots in use on the machine: when each of N concurrent heavy
// runs starts a runner with one worker per core, the machine carries N times its cores in runnable threads,
// and cores / N (at least 1) keeps it near load = cores. A queue that says nothing about the machine (no
// slot busy, no run, nothing configured) is left out; with nothing left there is nothing to suggest. When no
// queue used a heavy slot, the run being started is the one: cores / 1.
func SuggestCap(cores int, queues []QueueSlots) (WorkerCap, bool) {
	c := WorkerCap{Cores: cores, Queues: []CapQueue{}}
	for _, q := range queues {
		if q.Configured == 0 && q.BusyNow == 0 && q.WindowPeak == 0 {
			continue // nothing configured, busy or run there: the queue does not use this machine
		}
		cq := q.count()
		c.Queues = append(c.Queues, cq)
		c.HeavySlots += cq.Counted
	}
	if cores < 1 || len(c.Queues) == 0 {
		return WorkerCap{}, false
	}
	c.Workers = max(1, cores/max(1, c.HeavySlots))
	// The result leads, so a reader (or a clipped line) always has the number; the terms grow with the queues.
	c.Why = fmt.Sprintf("%d workers per run = %s (%s)", c.Workers, c.division(), strings.Join(c.terms(), "; "))
	return c, true
}

// division is the arithmetic without the per-queue terms: "12 cores / 4 heavy slots in use".
func (c WorkerCap) division() string {
	if c.HeavySlots == 0 {
		return fmt.Sprintf("%d cores / 1 run (no heavy slot in use now or in the window)", c.Cores)
	}
	return fmt.Sprintf("%d cores / %d heavy slots in use", c.Cores, c.HeavySlots)
}

// terms is each queue's count with how it was reached, in the order of c.Queues.
func (c WorkerCap) terms() []string {
	out := make([]string, 0, len(c.Queues))
	for _, q := range c.Queues {
		out = append(out, q.text())
	}
	return out
}

// PeakConcurrency is the most runs that were running at the same moment, each run being the interval
// [end - run_s, end), at least one second long (a history without run_s still says a run was there). A run
// that starts the second another ends took over its slot: the two do not overlap.
func PeakConcurrency(runs []Run) int {
	type edge struct {
		at    time.Time
		delta int
	}
	edges := make([]edge, 0, 2*len(runs))
	for _, r := range runs {
		d := time.Duration(max(r.RunS, 1) * float64(time.Second))
		edges = append(edges, edge{r.End.Add(-d), 1}, edge{r.End, -1})
	}
	slices.SortFunc(edges, func(a, b edge) int {
		if c := a.at.Compare(b.at); c != 0 {
			return c
		}
		return a.delta - b.delta // ends before starts at the same instant
	})
	peak, n := 0, 0
	for _, e := range edges {
		n += e.delta
		peak = max(peak, n)
	}
	return peak
}
