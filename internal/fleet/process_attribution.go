package fleet

import (
	"sort"
	"strings"

	"github.com/LysCongThanh/heavyq/internal/redact"
)

// OwnerHeavyq is heavyq's own queue in reports; OwnerOutside is no queue at all.
const (
	OwnerHeavyq  = "heavyq"
	OwnerOutside = "outside"
)

// Root is a process known to belong to a queue: a ticket's own process (on the machine the tickets live
// on), or the holder of a slot lock (any machine, Linux). Its descendants belong to the same lane.
type Root struct {
	Pid   int
	Owner string
	Lane  string
}

// Group is the processes of one owner, lane and command name on a machine.
type Group struct {
	Owner   string  `json:"owner"` // a queue name, or "outside"
	Lane    string  `json:"lane,omitempty"`
	Comm    string  `json:"comm"`
	Procs   int     `json:"procs"`
	Threads int     `json:"threads"` // 0 when unknown (macOS)
	CPUPct  float64 `json:"cpu_pct"` // one core = 100
	RSSMB   int     `json:"rss_mb"`
	Cmd     string  `json:"cmd,omitempty"` // the busiest member's command line, redacted and cut
}

// maxAncestry bounds the walk up the process tree (a loop in a racy ps snapshot must not spin).
const maxAncestry = 64

// Attribute groups every process of a probe by (owner, lane, comm), busiest first, and returns the RSS of
// the processes that belong to no queue. A process belongs to the nearest of its ancestors (itself
// included) that is a root, or that works in heavyq's remote work tree.
func Attribute(p Probe, roots []Root) (groups []Group, outsideRSSMB int) {
	byPid := make(map[int]Proc, len(p.Procs))
	for _, pr := range p.Procs {
		byPid[pr.Pid] = pr
	}
	rootOf := make(map[int]Root, len(roots))
	for _, r := range roots {
		if r.Pid > 0 {
			rootOf[r.Pid] = r
		}
	}
	type key struct{ owner, lane, comm string }
	acc := map[key]*Group{}
	best := map[key]float64{}
	outsideKB := 0
	for _, pr := range p.Procs {
		owner := ownerOf(pr.Pid, byPid, rootOf, p.InWorkTree)
		if owner.Owner == OwnerOutside {
			outsideKB += pr.RSSKB
		}
		k := key{owner.Owner, owner.Lane, pr.Comm}
		g := acc[k]
		if g == nil {
			g = &Group{Owner: k.owner, Lane: k.lane, Comm: k.comm}
			acc[k] = g
			best[k] = -1
		}
		g.Procs++
		g.Threads += pr.Threads
		g.CPUPct += pr.CPU
		g.RSSMB += pr.RSSKB // KB until the end
		if a, ok := p.Args[pr.Pid]; ok && pr.CPU > best[k] {
			best[k] = pr.CPU
			g.Cmd = clip(strings.Join(strings.Fields(redact.Command(a)), " "), 120)
		}
	}
	for _, g := range acc {
		g.RSSMB /= 1024
		g.CPUPct = round2(g.CPUPct)
		groups = append(groups, *g)
	}
	sort.Slice(groups, func(i, j int) bool {
		if groups[i].CPUPct != groups[j].CPUPct {
			return groups[i].CPUPct > groups[j].CPUPct
		}
		if groups[i].RSSMB != groups[j].RSSMB {
			return groups[i].RSSMB > groups[j].RSSMB
		}
		return groups[i].Owner+groups[i].Lane+groups[i].Comm < groups[j].Owner+groups[j].Lane+groups[j].Comm
	})
	return groups, outsideKB / 1024
}

// ownerOf walks up from pid to the nearest root or heavyq work-tree process.
func ownerOf(pid int, byPid map[int]Proc, roots map[int]Root, inWT map[int]bool) Root {
	for i := 0; i < maxAncestry && pid > 0; i++ {
		if r, ok := roots[pid]; ok {
			return r
		}
		if inWT[pid] {
			return Root{Pid: pid, Owner: OwnerHeavyq}
		}
		pr, ok := byPid[pid]
		if !ok || pr.PPid == pid {
			break
		}
		pid = pr.PPid
	}
	return Root{Owner: OwnerOutside}
}
