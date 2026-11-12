package flow

import "github.com/LysCongThanh/heavyq/internal/itemstatus"

// ItemCounts say where the anchor's items stand.
type ItemCounts struct {
	Total           int `json:"total"`
	Proven          int `json:"proven"`
	Running         int `json:"running"`          // a live lease holds it
	Blocked         int `json:"blocked"`          // `item block` or a needs: span
	ClaimedUnproven int `json:"claimed_unproven"` // ticked, and nothing ever proved it
	// DoneStale: ticked, and its proof passed on code that has changed since: it needs one more proof run, not
	// starting over (additive).
	DoneStale int `json:"done_stale"`
	Open      int `json:"open"` // nobody holds it and nothing proves it (stale, not ticked, included)
}

// countItems tallies the derived status of every item (stale and ticked is DoneStale, stale and not ticked is Open).
func countItems(items []ItemFact) ItemCounts {
	c := ItemCounts{Total: len(items)}
	for _, f := range items {
		switch f.Status.State {
		case itemstatus.Proven:
			c.Proven++
		case itemstatus.Claimed:
			c.Running++
		case itemstatus.Blocked:
			c.Blocked++
		case itemstatus.ClaimedDoneUnproven:
			c.ClaimedUnproven++
		case itemstatus.Stale:
			if f.Status.Done {
				c.DoneStale++
			} else {
				c.Open++ // proved once, never ticked: up for grabs like an open item
			}
		default: // open
			c.Open++
		}
	}
	return c
}
