package procinfo

import (
	"os"
	"sort"
	"strconv"
	"time"
)

// clockTicks is USER_HZ. It is 100 on every mainstream Linux build; reading
// it via sysconf would need cgo, which heavyq avoids for static binaries.
const clockTicks = 100.0

// maxAncestry bounds the parent walk (guards against cycles in fake trees).
const maxAncestry = 64

// pageKB is the memory page size in KiB (resident set sizes are in pages).
var pageKB = uint64(os.Getpagesize() / 1024)

// procSample is one process at one instant.
type procSample struct {
	ppid  int
	ticks uint64 // utime + stime
	comm  string
	rssKB uint64
}

// snapshot lists every process from the platform's source.
func (p ProcFS) snapshot() map[int]procSample {
	if p.UsePS {
		return psSnapshot()
	}
	return p.procSnapshot()
}

// sampleTwice takes two snapshots, interval apart, and returns the interval
// the CPU deltas span. /proc is read in microseconds, so that is the nominal
// interval; ps takes tens of milliseconds per run, so the measured time
// between the two reads is used instead.
func (p ProcFS) sampleTwice(interval time.Duration) (a, b map[int]procSample, span time.Duration) {
	a = p.snapshot()
	t0 := time.Now()
	time.Sleep(interval)
	b = p.snapshot()
	if p.UsePS {
		return a, b, time.Since(t0)
	}
	return a, b, interval
}

// procSnapshot reads every numeric entry under Root, zombies left out.
func (p ProcFS) procSnapshot() map[int]procSample {
	out := map[int]procSample{}
	ents, err := os.ReadDir(p.Root)
	if err != nil {
		return out
	}
	for _, e := range ents {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		comm, f, err := p.stat(pid)
		if err != nil || exited(f[0]) {
			continue // gone, or a zombie: no CPU, no memory, nothing to attribute
		}
		ppid, _ := strconv.Atoi(f[1])
		ut, _ := strconv.ParseUint(f[11], 10, 64)  // field 14
		st, _ := strconv.ParseUint(f[12], 10, 64)  // field 15
		rss, _ := strconv.ParseUint(f[21], 10, 64) // field 24, in pages
		out[pid] = procSample{ppid: ppid, ticks: ut + st, comm: comm, rssKB: rss * pageKB}
	}
	return out
}

// GroupCPU is the CPU share of one group ("lane X" or the outside label).
type GroupCPU struct {
	Group   string
	Percent float64
}

// Attribute samples all processes twice, interval apart, and sums each
// process's CPU% into the group of its nearest ancestor found in roots
// (pid -> group label). Processes with no such ancestor go to outside.
// Results are sorted by descending CPU.
func (p ProcFS) Attribute(roots map[int]string, outside string, interval time.Duration) []GroupCPU {
	a, b, span := p.sampleTwice(interval)
	return attribute(a, b, roots, outside, span)
}

func attribute(a, b map[int]procSample, roots map[int]string, outside string, interval time.Duration) []GroupCPU {
	sums := map[string]float64{}
	secs := interval.Seconds()
	if secs <= 0 {
		return nil
	}
	for pid, s1 := range b {
		s0, ok := a[pid]
		if !ok || s1.ticks <= s0.ticks {
			continue
		}
		pct := float64(s1.ticks-s0.ticks) * 100 / (clockTicks * secs)
		sums[groupOf(pid, b, roots, outside)] += pct
	}
	out := make([]GroupCPU, 0, len(sums))
	for g, v := range sums {
		out = append(out, GroupCPU{Group: g, Percent: v})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Percent != out[j].Percent {
			return out[i].Percent > out[j].Percent
		}
		return out[i].Group < out[j].Group
	})
	return out
}

func groupOf(pid int, tree map[int]procSample, roots map[int]string, outside string) string {
	for d := 0; d < maxAncestry && pid > 0; d++ {
		if g, ok := roots[pid]; ok {
			return g
		}
		s, ok := tree[pid]
		if !ok {
			break
		}
		pid = s.ppid
	}
	return outside
}
