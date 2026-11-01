package cli

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/LysCongThanh/heavyq/internal/queue"
)

// Terse (agent) waiting policy. An agent reads heavyq's stderr once, when the
// command ends, so a progress line only costs context: one line says it is
// queued (and only if it really has to wait), a rare heartbeat keeps a
// background watcher informed, and the admission line comes from execute.
//
// The first heartbeat is at 5 minutes, not 60 s: with a 60 s one a 2-minute
// wait prints four lines (queued, heartbeat, running, digest), over the
// three-line budget the tests enforce.
const (
	queuedAfter = 2 * time.Second
	beatFirst   = 5 * time.Minute
	beatEvery   = 5 * time.Minute
	aheadShown  = 3 // lanes named in the queued line
)

// waitView is what the wait loop knows when it asks the narrator to speak.
type waitView struct {
	pos, total int
	target     string
	prio       int
	lane       string
	ahead      []*queue.Ticket
}

// waitNarrator decides when a waiting ticket speaks and what it says. It takes
// the time as an argument, so the policy is tested with a fake clock.
type waitNarrator struct {
	w     io.Writer
	terse bool
	t0    time.Time
	// human: the last position said and when.
	lastPos string
	lastMsg time.Time
	// terse: whether the queued line was said, and when the next heartbeat is due.
	queued   bool
	nextBeat time.Time
}

func (n *waitNarrator) observe(now time.Time, v waitView) {
	if n.terse {
		n.observeTerse(now, v)
		return
	}
	if cur := fmt.Sprintf("%d/%d", v.pos, v.total); cur != n.lastPos || now.Sub(n.lastMsg) >= progressEvery {
		fmt.Fprintf(n.w, "heavyq: waiting %s (prio %d, lane %s, %s) for %s%s\n",
			cur, v.prio, v.lane, v.target, fmtDur(now.Sub(n.t0)), describeAhead(v.ahead))
		n.lastPos, n.lastMsg = cur, now
	}
}

func (n *waitNarrator) observeTerse(now time.Time, v waitView) {
	waited := now.Sub(n.t0)
	switch {
	case !n.queued && waited >= queuedAfter:
		fmt.Fprintf(n.w, "heavyq: queued #%d of %d on %s (p%d, lane %s)%s\n",
			v.pos, v.total, v.target, v.prio, v.lane, aheadShort(v.ahead))
		n.queued, n.nextBeat = true, n.t0.Add(beatFirst)
	case n.queued && !now.Before(n.nextBeat):
		fmt.Fprintf(n.w, "heavyq: still queued #%d of %d after %s (p%d, lane %s)\n",
			v.pos, v.total, fmtDur(waited), v.prio, v.lane)
		n.nextBeat = now.Add(beatEvery)
	}
}

// aheadShort names the first lanes in front of us: "; ahead: a(p3) b(p3) +2".
func aheadShort(ahead []*queue.Ticket) string {
	if len(ahead) == 0 {
		return ""
	}
	var parts []string
	for i, t := range ahead {
		if i == aheadShown {
			parts = append(parts, fmt.Sprintf("+%d", len(ahead)-aheadShown))
			break
		}
		parts = append(parts, fmt.Sprintf("%s(p%d)", t.Lane, t.Prio))
	}
	return "; ahead: " + strings.Join(parts, " ")
}
