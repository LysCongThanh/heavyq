package flow

import (
	"sort"
	"time"

	"github.com/LysCongThanh/heavyq/internal/history"
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

// foldBlocks turns the hook events into blocks, one open at a time per lane.
//
//   - A "blocked" event opens a block on its lane, or strengthens the open one:
//     the strongest class keeps the reason, the first event keeps Since.
//   - A "cleared" event (the lane's next tool call) closes it.
//   - So does the lane's next ledger run: a run that ended after the block began
//     means the agent went on. Until is then when that run started waiting.
//   - A block older than blockMaxAge with no answer is dropped: the session
//     is gone, nobody is being waited on.
func foldBlocks(events []Event, recs []history.Record, now time.Time) []Block {
	evs := append([]Event(nil), events...)
	sort.SliceStable(evs, func(i, j int) bool { return evs[i].At.Before(evs[j].At) })
	var all []Block
	open := map[string]int{} // lane -> index in all
	for _, e := range evs {
		if e.Lane == "" {
			continue
		}
		i, isOpen := open[e.Lane]
		switch e.Kind {
		case EventBlocked, EventStopFailure:
			class := e.Class
			if e.Kind == EventStopFailure {
				class = ClassStopFailure
			}
			if isOpen {
				if classRank[class] > classRank[all[i].Class] {
					all[i].Class, all[i].Reason = class, e.Reason
				}
				continue
			}
			open[e.Lane] = len(all)
			all = append(all, Block{Lane: e.Lane, Session: e.Session, Class: class, Reason: e.Reason, Since: e.At})
		case EventCleared:
			if isOpen {
				all[i].Until = e.At
				delete(open, e.Lane)
			}
		}
	}
	var out []Block
	for _, b := range all {
		if b.Open() {
			if run, ok := firstRunAfter(recs, b.Lane, b.Since); ok {
				b.Until = run
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

// firstRunAfter is when the first run of lane that ended after since began
// waiting (never before since itself).
func firstRunAfter(recs []history.Record, lane string, since time.Time) (time.Time, bool) {
	for _, r := range recs {
		if r.Lane != lane || !r.End.After(since) {
			continue
		}
		start := runStart(r).Add(-secs(r.WaitSecs))
		if start.Before(since) {
			start = since
		}
		return start, true
	}
	return time.Time{}, false
}

func secs(s float64) time.Duration { return time.Duration(s * float64(time.Second)) }

// runStart is when the command began running (after its wait in the queue).
func runStart(r history.Record) time.Time { return r.End.Add(-secs(r.RunSecs)) }
