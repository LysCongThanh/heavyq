package snapshot

import (
	"fmt"
	"hash/crc32"
	"strings"
	"time"
)

// Item states a chip can have: the states of internal/itemstatus that name a
// lane (an open or stale item nobody holds has no lane and no chip).
const (
	ItemClaimed = "claimed" // a live lease holds it
	ItemProven  = "proven"  // its proof passes; the lane has not run `item done` yet
	ItemBlocked = "blocked" // the lane recorded a need with `item block`
)

// LaneItems is what one lane holds or waits on in the work items of one
// repository (`heavyq item`). Added to Snapshot within schema 1.
type LaneItems struct {
	Lane  string     `json:"lane"`
	Repo  string     `json:"repo,omitempty"` // name of the repository's directory
	Items []ItemChip `json:"items"`
}

// ItemChip is one item of a lane.
type ItemChip struct {
	ID     string    `json:"id"`
	State  string    `json:"state"`
	Commit string    `json:"commit,omitempty"` // proven: the short commit
	Needs  string    `json:"needs,omitempty"`  // blocked: what a human must provide
	At     time.Time `json:"at,omitzero"`      // proven: when; blocked: since
}

// blockedItems: a lane recorded that an item needs something only a human can
// provide. It stays until a claim of the item answers it. One exception per
// item; its ID changes when the need does.
func blockedItems(s Snapshot) []Exception {
	var out []Exception
	for _, li := range s.Items {
		for _, it := range li.Items {
			if it.State != ItemBlocked {
				continue
			}
			text := fmt.Sprintf("%s is blocked on item `%s`: needs %s.", li.Lane, it.ID, strings.TrimSpace(it.Needs))
			out = append(out, Exception{
				ID:   strings.Join([]string{KindBlocked, li.Lane, it.ID, fmt.Sprintf("%08x", crc32.ChecksumIEEE([]byte(it.Needs)))}, "|"),
				Kind: KindBlocked, Severity: SeverityCritical, Lane: li.Lane, Text: text,
				Evidence: Evidence{At: it.At, Item: it.ID, Detail: it.Needs},
				// Once the need is met, claiming the item lifts the block.
				Action: &Action{Kind: ActionRun, Target: it.ID, Command: "heavyq item claim " + shellQuote(it.ID)},
			})
		}
	}
	return out
}
