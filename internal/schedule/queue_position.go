// Package schedule holds heavyq's pure scheduling rules: who may claim which
// machine, the resource gate, and when a command is safe to run remotely.
package schedule

import (
	"fmt"

	"github.com/LysCongThanh/heavyq/internal/queue"
)

// SlotKind is heavy or light.
type SlotKind int

const (
	Heavy SlotKind = iota
	Light
)

func (k SlotKind) String() string {
	if k == Light {
		return "light"
	}
	return "heavy"
}

// Opportunity says a ticket may try to claim a slot on Machine.
type Opportunity struct {
	Machine string
	Kinds   []SlotKind // slot kinds to try, in order
	// Head is true when no earlier ticket competes for this machine; only
	// head opportunities count toward anti-starvation.
	Head bool
	// Backfill is true when a light ticket jumps ahead of heavy tickets into
	// a light slot (heavy tickets never use light slots, so they lose nothing).
	Backfill bool
}

// SlotCounts gives the configured slot counts per machine.
type SlotCounts func(machine string) (heavy, light int)

// Opportunities computes, for ticket me with the tickets ahead of it in queue
// order, which candidate machines it may currently try and with which slot
// kinds. Machines it may not try get a human reason in blocked.
//
// Rules:
//   - an earlier ticket blocks me on machine M only if it may also use M;
//   - with no blocker, a heavy ticket tries heavy slots, a light ticket tries
//     light slots first and then heavy slots;
//   - with blockers, a light ticket may still backfill M's light slots if no
//     blocker is itself light.
func Opportunities(me *queue.Ticket, ahead []*queue.Ticket, counts SlotCounts) (opps []Opportunity, blocked map[string]string) {
	blocked = map[string]string{}
	for _, m := range me.Candidates {
		heavy, light := counts(m)
		var blockers, lightBlockers int
		for _, t := range ahead {
			if t.Uses(m) {
				blockers++
				if t.Light {
					lightBlockers++
				}
			}
		}
		switch {
		case blockers == 0:
			var kinds []SlotKind
			if me.Light && light > 0 {
				kinds = append(kinds, Light)
			}
			if heavy > 0 {
				kinds = append(kinds, Heavy)
			}
			if len(kinds) == 0 {
				blocked[m] = "no slot kind usable by this ticket"
				continue
			}
			opps = append(opps, Opportunity{Machine: m, Kinds: kinds, Head: true})
		case me.Light && light > 0 && lightBlockers == 0:
			opps = append(opps, Opportunity{Machine: m, Kinds: []SlotKind{Light}, Backfill: true})
		default:
			blocked[m] = fmt.Sprintf("%d ticket(s) ahead", blockers)
		}
	}
	return opps, blocked
}

// Ahead returns the waiting tickets before me and my 1-based position
// (0 when me is not in the list, e.g. pruned).
func Ahead(waiting []*queue.Ticket, me *queue.Ticket) ([]*queue.Ticket, int) {
	for i, t := range waiting {
		if t.Name == me.Name {
			return waiting[:i], i + 1
		}
	}
	return nil, 0
}
