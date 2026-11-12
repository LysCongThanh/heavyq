package itemstatus

import "github.com/LysCongThanh/heavyq/internal/lease"

// Marks folds the events log (oldest first) into one Mark per item. A claim
// or reclaim opens the item again: it clears an earlier block or done, which
// is how a blocked item is unblocked. Release and renew change nothing here.
// A sync event says the box of a deferred done has been ticked: the box
// carries the done from then on.
func Marks(events []lease.Event) map[string]Mark {
	out := map[string]Mark{}
	for _, e := range events {
		switch e.Op {
		case lease.OpClaim, lease.OpReclaim:
			delete(out, e.Item)
		case lease.OpBlock:
			out[e.Item] = Mark{Blocked: true, Needs: e.Needs, Lane: e.Lane, At: e.At}
		case lease.OpDone:
			out[e.Item] = Mark{Done: true, Deferred: e.Deferred, Commit: e.Commit, Lane: e.Lane, At: e.At}
		case lease.OpSync:
			if m, ok := out[e.Item]; ok && m.Done {
				m.Deferred = false
				out[e.Item] = m
			}
		}
	}
	return out
}
