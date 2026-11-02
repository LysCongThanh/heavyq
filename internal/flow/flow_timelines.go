package flow

import (
	"sort"
	"time"

	"github.com/LysCongThanh/heavyq/internal/history"
	"github.com/LysCongThanh/heavyq/internal/itemstatus"
)

// Timelines derives one timeline per work item that left a trace, then one per
// session (else lane) for the runs no item claimed. Every run and every blocked
// stretch lands in exactly one timeline, so fleet lead time never counts the
// same minute twice.
//
// A run belongs to the item named by its ledger tag, else to the item its lane
// held when it started (HEAVYQ_ITEM is often unset). Items come first, sorted
// by id, then sessions, sorted by key.
func Timelines(in Input) []Timeline {
	tls, _ := derive(in)
	return tls
}

// derive is Timelines, and the lease holds it folded on the way (the
// throughput needs the claims).
func derive(in Input) ([]Timeline, map[string]*itemHolds) {
	facts := make(map[string]ItemFact, len(in.Items))
	for _, f := range in.Items {
		facts[f.Status.ID] = f
	}
	holds := holdsOf(in.Leases, facts, in.Now)
	blocks := in.blocks()

	itemRecs, sessRecs := map[string][]history.Record{}, map[string][]history.Record{}
	for _, r := range in.Records {
		id := r.Item
		if id == "" {
			id = holderAt(holds, r.Lane, runStart(r))
		}
		if id != "" {
			itemRecs[id] = append(itemRecs[id], r)
		} else {
			k := sessionKey(r.Session, r.Lane)
			sessRecs[k] = append(sessRecs[k], r)
		}
	}

	ids := map[string]bool{}
	for id, ih := range holds {
		if len(ih.holds) > 0 || len(ih.blocks) > 0 {
			ids[id] = true
		}
	}
	for id := range itemRecs {
		ids[id] = true
	}
	var out []Timeline
	for _, id := range sortedKeys(ids) {
		if t, ok := itemTimeline(in, id, facts[id], holds[id], itemRecs[id], blocks); ok {
			out = append(out, t)
		}
	}
	out = append(out, sessionTimelines(in, sessRecs, holds, blocks)...)
	return out, holds
}

// sessionKey names a session timeline: the session id, else the lane.
func sessionKey(session, lane string) string {
	if session != "" {
		return session
	}
	return "lane:" + lane
}

// itemTimeline paints the candidate spans of one item.
func itemTimeline(in Input, id string, fact ItemFact, ih *itemHolds, recs []history.Record, blocks []Block) (Timeline, bool) {
	t := Timeline{Kind: KindItem, Key: id}
	var c []Span
	var holdEnds []time.Time
	if ih != nil {
		for _, h := range ih.holds {
			c = append(c, Span{Working, h.iv.start, h.iv.end})
			holdEnds = append(holdEnds, h.iv.end)
			t.Lane, t.Session = h.lane, h.session
			for _, b := range blocks { // a prompt while the lane held the item is the item's wait
				if b.Lane == h.lane {
					if x := intersect(b.span(in.Now), h.iv); !x.empty() {
						c = append(c, Span{BlockedHuman, x.start, x.end})
					}
				}
			}
		}
		for _, iv := range ih.blocks {
			c = append(c, Span{BlockedHuman, iv.start, iv.end})
		}
	}
	if len(recs) > 0 && t.Lane == "" {
		t.Lane, t.Session = recs[len(recs)-1].Lane, recs[len(recs)-1].Session
	}
	tail := func(failedAt time.Time) time.Time { // a failed proof with no retry stays rework while the lease lasts
		for _, e := range holdEnds {
			if e.After(failedAt) {
				return e
			}
		}
		return failedAt
	}
	c = append(c, recordSpans(recs, fact, tail)...)
	for _, e := range in.External {
		if e.Item == id {
			c = append(c, e.Span)
		}
	}
	t.Spans = paint(c)
	switch fact.Status.State {
	case itemstatus.Proven:
		t.Final, t.FinalAt = DoneProven, fact.Status.At
	case itemstatus.ClaimedDoneUnproven:
		t.Final, t.FinalAt = ClaimedUnproven, fact.Status.At
	}
	if t.Final != "" && t.FinalAt.IsZero() && len(t.Spans) > 0 { // the status carries no time: the end of the work
		t.FinalAt = t.Spans[len(t.Spans)-1].End
	}
	return t, len(t.Spans) > 0
}

// sessionTimelines builds the timelines of runs that belong to no item, and of
// blocked stretches outside any item's lease.
func sessionTimelines(in Input, sessRecs map[string][]history.Record, holds map[string]*itemHolds, blocks []Block) []Timeline {
	byKey := map[string]*Timeline{}
	cands := map[string][]Span{}
	touch := func(key, lane, session string) {
		if byKey[key] == nil {
			byKey[key] = &Timeline{Kind: KindSession, Key: key, Lane: lane, Session: session}
		}
	}
	for key, recs := range sessRecs {
		last := recs[len(recs)-1]
		touch(key, last.Lane, last.Session)
		tail := func(failedAt time.Time) time.Time { return minTime(in.Now, failedAt.Add(ActiveGap)) }
		cands[key] = append(cands[key], recordSpans(recs, ItemFact{}, tail)...)
		cands[key] = append(cands[key], activeGaps(recs, in.Now)...)
	}
	var laneHolds = map[string][]interval{}
	for _, ih := range holds {
		for _, h := range ih.holds {
			laneHolds[h.lane] = append(laneHolds[h.lane], h.iv)
		}
	}
	for _, b := range blocks {
		for _, iv := range subtract(b.span(in.Now), laneHolds[b.Lane]) {
			key := sessionKey(b.Session, b.Lane)
			touch(key, b.Lane, b.Session)
			cands[key] = append(cands[key], Span{BlockedHuman, iv.start, iv.end})
		}
	}
	var out []Timeline
	for _, key := range sortedKeys(keysSet(byKey)) {
		t := byKey[key]
		t.Spans = paint(cands[key])
		if len(t.Spans) > 0 {
			out = append(out, *t)
		}
	}
	return out
}

// activeGaps marks the silence between two runs of a session as working when it
// is short (the agent was thinking or editing), and the time since the last run
// too while it is. Longer silences are idle and belong to no state.
func activeGaps(recs []history.Record, now time.Time) []Span {
	var out []Span
	for i, r := range recs {
		next := now
		if i+1 < len(recs) {
			n := recs[i+1]
			next = runStart(n).Add(-secs(n.WaitSecs))
		}
		if gap := next.Sub(r.End); gap > 0 && gap <= ActiveGap {
			out = append(out, Span{Working, r.End, next})
		}
	}
	return out
}

// recordSpans are the candidate spans of ledger runs: queued while waiting for
// a slot, proving (a gate or the item's proof) or working (anything else), and
// rework from a failed proof to the next attempt, or to tail(failure) when
// there is none.
func recordSpans(recs []history.Record, fact ItemFact, tail func(time.Time) time.Time) []Span {
	sorted := append([]history.Record(nil), recs...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].End.Before(sorted[j].End) })
	var out []Span
	var proofs []history.Record
	for _, r := range sorted {
		rs := runStart(r)
		ws := rs.Add(-secs(r.WaitSecs))
		out = append(out, Span{Queued, ws, rs})
		if r.Outcome() != history.OutcomeRan {
			continue // left the queue without running: only the wait counts
		}
		if isProof(r, fact) {
			out = append(out, Span{Proving, rs, r.End})
			proofs = append(proofs, r)
		} else {
			out = append(out, Span{Working, rs, r.End})
		}
	}
	for i, p := range proofs {
		if p.Exit == 0 {
			continue
		}
		end := tail(p.End)
		if i+1 < len(proofs) {
			n := proofs[i+1]
			end = runStart(n).Add(-secs(n.WaitSecs))
		}
		out = append(out, Span{Rework, p.End, end})
	}
	return out
}

// isProof: the run is a gate run, or runs the item's declared proof.
func isProof(r history.Record, f ItemFact) bool {
	if r.Gate != "" {
		return true
	}
	for _, c := range f.ProofCmds {
		if c != "" && r.Cmd == c {
			return true
		}
	}
	return false
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func keysSet[V any](m map[string]V) map[string]bool {
	out := make(map[string]bool, len(m))
	for k := range m {
		out[k] = true
	}
	return out
}
