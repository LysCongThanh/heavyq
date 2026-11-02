package flow

import (
	"sort"
	"time"

	"github.com/LysCongThanh/heavyq/internal/history"
	"github.com/LysCongThanh/heavyq/internal/lease"
)

// Block is a stretch during which a lane's agent waited on a person.
type Block struct {
	Lane    string
	Session string
	Class   string
	Reason  string
	Since   time.Time
	Until   time.Time // zero while it is open
}

// Open reports whether nobody has answered yet.
func (b Block) Open() bool { return b.Until.IsZero() }

func (b Block) span(now time.Time) interval {
	if b.Open() {
		return interval{b.Since, now}
	}
	return interval{b.Since, b.Until}
}

// activity is a sign that the agent of a lane or session went on: a ledger run
// (from when it began waiting for a slot to its end) or a lease taken, renewed
// or released (an instant).
type activity struct {
	Start, End    time.Time
	Lane, Session string
}

// activitiesOf are the signs of life in the ledger and the lease log, oldest first.
func activitiesOf(recs []history.Record, leases []lease.Event) []activity {
	out := make([]activity, 0, len(recs)+len(leases))
	for _, r := range recs {
		out = append(out, activity{Start: runStart(r).Add(-secs(r.WaitSecs)), End: r.End, Lane: r.Lane, Session: r.Session})
	}
	for _, e := range leases {
		out = append(out, activity{Start: e.At, End: e.At, Lane: e.Lane, Session: e.Session})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Start.Before(out[j].Start) })
	return out
}

// blocks are the blocks of in, folded from the hook events; an open one of a
// lane whose work tree is gone is closed at once (nobody is there to answer).
func (in Input) blocks() []Block {
	out := foldBlocksFrom(in.Hooks, activitiesOf(in.Records, in.Leases), in.Now)
	for i := range out {
		if out[i].Open() && in.GoneLanes[out[i].Lane] {
			out[i].Until = out[i].Since
		}
	}
	return out
}

// foldBlocks is foldBlocksFrom over ledger runs alone.
func foldBlocks(events []Event, recs []history.Record, now time.Time) []Block {
	return foldBlocksFrom(events, activitiesOf(recs, nil), now)
}

// foldBlocksFrom turns the hook events into blocks, one open at a time per lane.
//
//   - A "blocked" event opens a block on its lane, or strengthens the open one:
//     the strongest class keeps the reason, the first event keeps Since.
//   - A stop on a usage limit opens none: that is the whole fleet's, one entry
//     (deriveQuota), not one per lane.
//   - A "cleared" event (the next tool call of a session) closes its lane's
//     block and every open block of the same session, whatever lane it was
//     recorded under: a session that moved between work trees is one agent.
//   - So does later activity of the lane or of the block's session: a run that
//     ended after the block began, or a lease taken or renewed, means the agent
//     went on. Until is then when that activity began (never before the block).
//   - A block older than blockMaxAge with no answer is dropped: the session
//     is gone, nobody is being waited on.
func foldBlocksFrom(events []Event, acts []activity, now time.Time) []Block {
	evs := append([]Event(nil), events...)
	sort.SliceStable(evs, func(i, j int) bool { return evs[i].At.Before(evs[j].At) })
	var all []Block
	open := map[string]int{} // lane -> index in all
	for _, e := range evs {
		switch e.Kind {
		case EventBlocked, EventStopFailure:
			if e.Lane == "" || isQuotaStop(e) {
				continue
			}
			class := e.Class
			if e.Kind == EventStopFailure {
				class = ClassStopFailure
			}
			if i, isOpen := open[e.Lane]; isOpen {
				if classRank[class] > classRank[all[i].Class] {
					all[i].Class, all[i].Reason = class, e.Reason
				}
				continue
			}
			open[e.Lane] = len(all)
			all = append(all, Block{Lane: e.Lane, Session: e.Session, Class: class, Reason: e.Reason, Since: e.At})
		case EventCleared:
			for lane, i := range open {
				if lane == e.Lane || (e.Session != "" && all[i].Session == e.Session) {
					all[i].Until = e.At
					delete(open, lane)
				}
			}
		}
	}
	var out []Block
	for _, b := range all {
		if b.Open() {
			if at, ok := firstActivityAfter(acts, b); ok {
				b.Until = at
			} else if now.Sub(b.Since) > blockMaxAge {
				continue
			}
		}
		if !b.Open() && b.Until.Before(b.Since) {
			b.Until = b.Since
		}
		out = append(out, b)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Since.Before(out[j].Since) })
	return out
}

// firstActivityAfter is when the first sign of life of the block's lane or
// session that reached past the block's start began (never before it).
func firstActivityAfter(acts []activity, b Block) (time.Time, bool) {
	for _, a := range acts {
		if !a.End.After(b.Since) || (a.Lane != b.Lane && (b.Session == "" || a.Session != b.Session)) {
			continue
		}
		if a.Start.Before(b.Since) {
			return b.Since, true
		}
		return a.Start, true
	}
	return time.Time{}, false
}

func secs(s float64) time.Duration { return time.Duration(s * float64(time.Second)) }

// runStart is when the command began running (after its wait in the queue).
func runStart(r history.Record) time.Time { return r.End.Add(-secs(r.RunSecs)) }
