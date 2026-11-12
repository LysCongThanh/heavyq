package queue

import (
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/LysCongThanh/heavyq/internal/procinfo"
)

// Owners is what one survey of the process table found about the processes
// that own tickets, by pid.
type Owners map[int]procinfo.Owner

// Stopped reports whether the owner of t is stopped (Ctrl-Z, SIGSTOP, a
// debugger). A stopped waiter runs no scheduling rounds until it is resumed,
// so it cannot claim anything.
func (o Owners) Stopped(t *Ticket) bool {
	w, ok := o[t.Pid]
	return ok && w.Start == t.Start && w.Stopped
}

// Active returns ts without the tickets whose owner is stopped. A stopped
// waiter keeps its ticket and its place, since it may be resumed, but it must
// not count as a ticket ahead: it would hold back every ticket behind it for
// as long as it stays stopped, and only its owner can cancel it.
func (o Owners) Active(ts []*Ticket) []*Ticket {
	out := make([]*Ticket, 0, len(ts))
	for _, t := range ts {
		if !o.Stopped(t) {
			out = append(out, t)
		}
	}
	return out
}

// Prune removes tickets whose owning process is gone (or whose pid now
// belongs to another process), and session files older than SessionTTL, and
// returns the owners it found. src is procinfo.Default in production. It
// checks every owner in one survey: on macOS that is one ps run, not one per
// ticket (every waiter prunes every poll). When the process table cannot be
// read at all, nothing is removed.
func (d *Dir) Prune(src procinfo.ProcFS) Owners {
	d.pruneSessions(time.Now())
	s, err := d.List()
	if err != nil {
		return Owners{}
	}
	all := append(s.Waiting, s.Running...)
	pids := make([]int, 0, len(all))
	for _, t := range all {
		pids = append(pids, t.Pid)
	}
	found, ok := src.Survey(pids)
	if !ok {
		return Owners{}
	}
	for _, t := range all {
		if w, live := found[t.Pid]; !live || t.Start == "" || w.Start != t.Start {
			d.Remove(t)
		}
	}
	return Owners(found)
}

// enqueueTime is the time a new ticket is named after: now, or just after the
// newest ticket in the queue when the wall clock is behind it (stepped back by
// NTP, a VM resume or a manual date). The name is the queue order, so a new
// ticket must never sort before one that was enqueued earlier.
func (d *Dir) enqueueTime(now time.Time) time.Time {
	ents, err := os.ReadDir(d.Path)
	if err != nil {
		return now
	}
	newest := now.UnixNano() - 1
	for _, e := range ents {
		n := strings.TrimSuffix(e.Name(), runSuffix)
		if !ticketName.MatchString(n) {
			continue
		}
		if ns, err := strconv.ParseInt(n[2:22], 10, 64); err == nil && ns > newest {
			newest = ns
		}
	}
	return time.Unix(0, newest+1)
}
