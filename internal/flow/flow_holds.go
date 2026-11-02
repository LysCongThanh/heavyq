package flow

import (
	"sort"
	"time"

	"github.com/LysCongThanh/heavyq/internal/itemstatus"
	"github.com/LysCongThanh/heavyq/internal/lease"
)

// hold is one stretch during which a lane's session held an item's lease.
type hold struct {
	item    string
	lane    string
	session string
	iv      interval
}

// itemHolds is what the lease events say about one item.
type itemHolds struct {
	holds []hold
	// blocks are the stretches `item block` left the item waiting on a need: from
	// the block to the next claim (or to now, while the item still reads blocked).
	blocks []interval
	// claims are the times a lease was taken (claim or reclaim), oldest first.
	claims []time.Time
}

// holdsOf folds the lease events of every item. An open hold ends at now when
// the item's status says a live lease holds it, else one lease TTL after the
// last sign of life (a claim or renewal): an expired lease holds nothing.
func holdsOf(events []lease.Event, facts map[string]ItemFact, now time.Time) map[string]*itemHolds {
	evs := append([]lease.Event(nil), events...)
	sort.SliceStable(evs, func(i, j int) bool { return evs[i].At.Before(evs[j].At) })
	type openHold struct {
		h    hold
		seen time.Time
	}
	open := map[string]*openHold{}
	blockedAt := map[string]time.Time{}
	out := map[string]*itemHolds{}
	get := func(id string) *itemHolds {
		if out[id] == nil {
			out[id] = &itemHolds{}
		}
		return out[id]
	}
	closeHold := func(id string, at time.Time) {
		if o := open[id]; o != nil {
			if at.After(o.h.iv.start) {
				o.h.iv.end = at
				get(id).holds = append(get(id).holds, o.h)
			}
			delete(open, id)
		}
	}
	for _, e := range evs {
		ih := get(e.Item)
		switch e.Op {
		case lease.OpClaim, lease.OpReclaim:
			if t, ok := blockedAt[e.Item]; ok { // answered: someone took the item again
				ih.blocks = append(ih.blocks, interval{t, e.At})
				delete(blockedAt, e.Item)
			}
			if o := open[e.Item]; o != nil && o.h.lane == e.Lane && o.h.session == e.Session {
				o.seen = e.At // the same owner claiming again is a renewal
				continue
			}
			closeHold(e.Item, e.At)
			ih.claims = append(ih.claims, e.At)
			open[e.Item] = &openHold{h: hold{item: e.Item, lane: e.Lane, session: e.Session, iv: interval{start: e.At}}, seen: e.At}
		case lease.OpRenew:
			if o := open[e.Item]; o != nil {
				o.seen = e.At
			} else { // the claim fell out of the window the caller read
				open[e.Item] = &openHold{h: hold{item: e.Item, lane: e.Lane, session: e.Session, iv: interval{start: e.At}}, seen: e.At}
			}
		case lease.OpRelease, lease.OpDone:
			closeHold(e.Item, e.At)
		case lease.OpBlock:
			closeHold(e.Item, e.At)
			blockedAt[e.Item] = e.At
		}
	}
	for id, o := range open {
		end := o.seen.Add(lease.DefaultTTL)
		if f, ok := facts[id]; ok && f.Status.State == itemstatus.Claimed {
			end = now
		}
		if end.After(now) {
			end = now
		}
		closeHold(id, end)
	}
	for id, t := range blockedAt {
		if f, ok := facts[id]; ok && f.Status.State == itemstatus.Blocked {
			get(id).blocks = append(get(id).blocks, interval{t, now})
		}
	}
	return out
}

// holderAt is the item a lane held when t happened ("" when none).
// With several at once the smallest id is taken, so the answer is stable.
func holderAt(byItem map[string]*itemHolds, lane string, t time.Time) string {
	best := ""
	for id, ih := range byItem {
		for _, h := range ih.holds {
			if h.lane == lane && !t.Before(h.iv.start) && t.Before(h.iv.end) && (best == "" || id < best) {
				best = id
			}
		}
	}
	return best
}

// lastClaim is the claim a proof at `at` most plausibly followed: the latest
// claim not after it.
func (ih *itemHolds) lastClaim(at time.Time) (time.Time, bool) {
	var best time.Time
	for _, c := range ih.claims {
		if !c.After(at) && c.After(best) {
			best = c
		}
	}
	return best, !best.IsZero()
}
