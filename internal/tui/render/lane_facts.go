package render

import (
	"strconv"

	"github.com/LysCongThanh/heavyq/internal/snapshot"
)

// facts are the displayable values of one ticket row.
type facts struct {
	running  bool
	lane     string
	prio     int
	where    string  // machine/slot when running, "#position target" when waiting
	waitS    float64 // time in the queue (still counting when waiting)
	runS     float64 // time running; 0 when waiting
	reason   string  // waiting: why it cannot start
	bypass   string  // waiting head: "bypass in 5m"
	cmd      string
	forced   bool
	canceled bool
	gate     string
	gateSt   Style
	item     string
	itemSt   Style
}

// factsOf reads a ticket Row, adding the live seconds since the snapshot.
func (c *ctx) factsOf(r Row) facts {
	var f facts
	switch {
	case r.Running != nil:
		t := r.Running
		f = facts{running: true, lane: t.Lane, prio: t.Prio, cmd: t.Cmd, forced: t.Forced, canceled: t.CancelRequested,
			where: t.Machine + "/" + shortSlot(t.Slot), waitS: t.WaitS, runS: t.RunS + c.elapsed}
	case r.Wait != nil:
		t := r.Wait
		f = facts{lane: t.Lane, prio: t.Prio, cmd: t.Cmd, canceled: t.CancelRequested,
			where: "#" + strconv.Itoa(t.Position) + " " + t.Target, waitS: t.WaitS + c.elapsed, reason: waitReason(*t)}
		if t.BypassInS >= 0 && t.Position == 1 {
			f.bypass = "bypass in " + dur(t.BypassInS-c.elapsed)
		}
	}
	f.gate, f.gateSt = c.laneGate(f.lane)
	f.item, f.itemSt = c.laneItem(f.lane)
	return f
}

// laneGate is the proof state of the lane's main gate: FAILED on a tree,
// proven, unproven; "" when it was skipped or unknown.
func (c *ctx) laneGate(lane string) (string, Style) {
	for _, g := range c.s.Gates {
		if g.Lane != lane {
			continue
		}
		switch g.State {
		case snapshot.GateFailed:
			if g.Tree != "" {
				return "FAILED " + shortTree(g.Tree), Bad | Bold
			}
			return "FAILED", Bad | Bold
		case snapshot.GateProven:
			return "proven", Good
		case snapshot.GateUnproven:
			return "unproven", Dim
		}
	}
	return "", 0
}

// laneItem is the first work item the lane holds or waits on.
func (c *ctx) laneItem(lane string) (string, Style) {
	for _, li := range c.s.Items {
		if li.Lane != lane || len(li.Items) == 0 {
			continue
		}
		it := li.Items[0]
		if it.State == snapshot.ItemBlocked {
			return it.ID + " (blocked)", Bad
		}
		return it.ID, Dim
	}
	return "", 0
}

// detail is the flexible last column: the command, and for a waiting ticket
// its reason first (the reason is why you look at the row).
func (c *ctx) detail(f facts, w int) Line {
	var l Line
	add := func(s Seg) {
		if l.Width() > 0 {
			l = append(l, blanks(2))
		}
		l = append(l, s)
	}
	if !f.running {
		add(c.seg(Warn, f.reason))
		if f.bypass != "" && l.Width()+2+cellWidth(f.bypass) <= w {
			add(c.seg(Dim, "("+f.bypass+")"))
		}
	}
	if f.canceled {
		add(c.seg(Warn, "[cancelling]"))
	}
	if f.forced {
		add(c.seg(Warn, "[forced]"))
	}
	if rest := w - l.Width() - 2; f.cmd != "" && (rest >= minFlex || l.Width() == 0) {
		add(c.seg(0, f.cmd))
	}
	return l
}
