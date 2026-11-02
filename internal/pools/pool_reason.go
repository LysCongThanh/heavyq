package pools

import (
	"fmt"
	"strings"
)

// ReasonPrefix starts every wait reason a pool gives, so that the story and
// the dashboard can tell it from a RAM, load or slot reason.
const ReasonPrefix = "waiting for "

// IsReason reports whether a wait reason comes from a pool.
func IsReason(r string) bool { return strings.HasPrefix(r, ReasonPrefix) }

// maxHolderNames bounds the lanes a reason names.
const maxHolderNames = 4

// Reason words the shortfall as a wait reason:
//
//	waiting for pg: 3/3 held by a, b, c
//	waiting for port (needs 2): 3/4 held by a (1 in use outside the queue)
//
// lanes are the lanes of the running tickets that hold values of the pool,
// byTickets how many values they hold together; what the pool is short of
// beyond that is in use by something outside the queue (a bound port, a
// legacy lock).
func (s Shortfall) Reason(lanes []string, byTickets int) string {
	held := s.Capacity - s.Free
	need := ""
	if s.Need > 1 {
		need = fmt.Sprintf(" (needs %d)", s.Need)
	}
	out := fmt.Sprintf("%s%s%s: %d/%d held", ReasonPrefix, s.Pool, need, held, s.Capacity)
	if len(lanes) > maxHolderNames {
		lanes = append(lanes[:maxHolderNames:maxHolderNames], fmt.Sprintf("+%d more", len(lanes)-maxHolderNames))
	}
	if len(lanes) > 0 {
		out += " by " + strings.Join(lanes, ", ")
	}
	if outside := held - byTickets; outside > 0 {
		out += fmt.Sprintf(" (%d in use outside the queue)", outside)
	}
	return out
}
