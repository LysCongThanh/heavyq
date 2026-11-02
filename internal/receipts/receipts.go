// Package receipts summarises what heavyq did over a window of the ledger:
// exact counts (Compute) and a privacy-rounded version of them (Share). It is
// pure: it reads records the caller loaded and touches no file.
//
// Every metric counts only what the ledger proves. docs/receipts.md defines
// each one, and says why "freezes avoided" is not among them.
package receipts

import (
	"strings"
	"time"

	"github.com/LysCongThanh/heavyq/internal/config"
	"github.com/LysCongThanh/heavyq/internal/history"
)

// HeldBackMinWaitS is the shortest queue wait that counts as "held back".
// Admission is a polling loop, so a run that waited a moment for the next
// round did not wait for anything.
const HeldBackMinWaitS = 1.0

// Receipts is the exact summary of the records that ended in [From, To).
// It carries counts only: never a lane, repository, command, path or machine
// name.
type Receipts struct {
	From time.Time `json:"from"`
	To   time.Time `json:"to"`
	// Runs: heavy commands that ran through the queue (not --light, not
	// replayed from the cache, not given up or cancelled while waiting).
	Runs int `json:"runs"`
	// DuplicatesAvoided = CacheHits + Coalesced: runs that reused the result of
	// an identical run instead of executing.
	DuplicatesAvoided int `json:"duplicates_avoided"`
	CacheHits         int `json:"cache_hits"`
	Coalesced         int `json:"coalesced"`
	// HeldBack: runs that waited at least HeldBackMinWaitS in the queue before
	// starting (for a slot, RAM or load: the ledger does not say which).
	HeldBack int `json:"held_back"`
	// Flaky: distinct run ids that both passed and failed in the window with a
	// failure the machine's load does not explain.
	Flaky int `json:"flaky"`
	// UnderLoad: failed runs that started while the machine was over a limit
	// of the resource gate (see history.Record.Loaded).
	UnderLoad int `json:"under_load"`
	// RemoteRuns ran on a machine other than "local"; RemoteMachines is how
	// many such machines.
	RemoteRuns     int `json:"remote_runs"`
	RemoteMachines int `json:"remote_machines"`
	// PoolValues: pool values (ports, databases, ...) held by runs.
	PoolValues int     `json:"pool_values"`
	WaitP50S   float64 `json:"wait_p50_s"`
	WaitP95S   float64 `json:"wait_p95_s"`
	// Lanes: distinct lanes (agents) with at least one run or reuse.
	Lanes int `json:"lanes"`
}

// Days is the length of the window in days.
func (r Receipts) Days() float64 { return r.To.Sub(r.From).Hours() / 24 }

// runVerdict is what the window shows of one run id.
type runVerdict struct{ passed, failedUnexplained bool }

// Compute summarises the records that ended in [from, to).
func Compute(recs []history.Record, from, to time.Time) Receipts {
	out := Receipts{From: from, To: to}
	lanes := map[string]bool{}
	remote := map[string]bool{}
	byRun := map[string]*runVerdict{}
	var waits []float64
	for _, r := range recs {
		if r.End.Before(from) || !r.End.Before(to) || r.Light {
			continue
		}
		switch r.Outcome() {
		case history.OutcomeGaveUp, history.OutcomeCancelled:
			continue
		case history.OutcomeInterrupted:
			if r.Machine == "" { // stopped while still waiting: not a run
				continue
			}
		}
		if r.Lane != "" {
			lanes[r.Lane] = true
		}
		switch r.Cache {
		case "hit":
			out.CacheHits++
			continue
		case "coalesced":
			out.Coalesced++
			continue
		}
		out.Runs++
		waits = append(waits, r.WaitSecs)
		if r.WaitSecs >= HeldBackMinWaitS {
			out.HeldBack++
		}
		if r.Machine != "" && r.Machine != config.LocalName {
			out.RemoteRuns++
			remote[r.Machine] = true
		}
		out.PoolValues += poolValues(r.Pools)
		if r.Outcome() != history.OutcomeRan {
			continue
		}
		failed := r.Exit != 0
		if failed && r.Loaded() {
			out.UnderLoad++
		}
		if r.RunID == "" {
			continue
		}
		v := byRun[r.RunID]
		if v == nil {
			v = &runVerdict{}
			byRun[r.RunID] = v
		}
		v.passed = v.passed || !failed
		v.failedUnexplained = v.failedUnexplained || (failed && !r.Loaded())
	}
	for _, v := range byRun {
		if v.passed && v.failedUnexplained {
			out.Flaky++
		}
	}
	out.DuplicatesAvoided = out.CacheHits + out.Coalesced
	out.RemoteMachines = len(remote)
	out.Lanes = len(lanes)
	out.WaitP50S, out.WaitP95S = percentile(waits, 50), percentile(waits, 95)
	return out
}

// poolValues counts the values in a Record.Pools string ("port=4100,4101 pg=pg2").
func poolValues(pools string) int {
	n := 0
	for _, f := range strings.Fields(pools) {
		if _, vals, ok := strings.Cut(f, "="); ok && vals != "" {
			n += strings.Count(vals, ",") + 1
		}
	}
	return n
}
