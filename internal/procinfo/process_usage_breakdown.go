package procinfo

import (
	"sort"
	"time"
)

// GroupUsage is the CPU and resident memory of a group of processes.
type GroupUsage struct {
	Group  string
	Procs  int
	CPUPct float64 // 100 = one core
	RSSMB  int
}

// Breakdown samples all processes twice, interval apart. Each process is
// charged to the label of its nearest ancestor in roots (pid -> label);
// processes without one are grouped by command name in outside. Kernel
// threads are ignored. Both lists are sorted by resident memory, largest
// first.
func (p ProcFS) Breakdown(roots map[int]string, interval time.Duration) (byRoot, outside []GroupUsage) {
	a := p.snapshot()
	time.Sleep(interval)
	b := p.snapshot()
	return breakdown(a, b, roots, interval)
}

func breakdown(a, b map[int]procSample, roots map[int]string, interval time.Duration) (byRoot, outside []GroupUsage) {
	const none = "\x00"
	inRoot, out := map[string]*GroupUsage{}, map[string]*GroupUsage{}
	secs := interval.Seconds()
	for pid, s1 := range b {
		if pid == 2 || s1.ppid == 2 { // kthreadd and kernel threads
			continue
		}
		var pct float64
		if s0, ok := a[pid]; ok && s1.ticks > s0.ticks && secs > 0 {
			pct = float64(s1.ticks-s0.ticks) * 100 / (clockTicks * secs)
		}
		groups, name := inRoot, groupOf(pid, b, roots, none)
		if name == none {
			groups, name = out, s1.comm
		}
		g := groups[name]
		if g == nil {
			g = &GroupUsage{Group: name}
			groups[name] = g
		}
		g.Procs++
		g.CPUPct += pct
		g.RSSMB += int(s1.rssKB / 1024)
	}
	return sortedUsage(inRoot), sortedUsage(out)
}

func sortedUsage(m map[string]*GroupUsage) []GroupUsage {
	out := make([]GroupUsage, 0, len(m))
	for _, g := range m {
		out = append(out, *g)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].RSSMB != out[j].RSSMB {
			return out[i].RSSMB > out[j].RSSMB
		}
		return out[i].Group < out[j].Group
	})
	return out
}

// Descendants returns the pids of every process below pid (children,
// grandchildren, ...), parents before children. Empty without /proc.
func (p ProcFS) Descendants(pid int) []int {
	tree := p.snapshot()
	kids := map[int][]int{}
	for c, s := range tree {
		kids[s.ppid] = append(kids[s.ppid], c)
	}
	var out []int
	queue := []int{pid}
	for len(queue) > 0 && len(out) < 4096 {
		cur := queue[0]
		queue = queue[1:]
		for _, c := range kids[cur] {
			out = append(out, c)
			queue = append(queue, c)
		}
	}
	return out
}
