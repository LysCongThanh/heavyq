package fleet

import (
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

// statTickSlack is what one CPU-time reading from /proc is off by at most: one clock tick at USER_HZ 100.
const statTickSlack = 10 * time.Millisecond

// statCPU reads pid's CPU time (utime+stime) straight from /proc/<pid>/stat: the test's own reading of
// the kernel's counters, not the probe script's.
func statCPU(t *testing.T, pid int) time.Duration {
	t.Helper()
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	f := strings.Fields(s[strings.LastIndexByte(s, ')')+1:]) // after comm: state ppid ... utime(14) stime(15)
	if len(f) < 13 {
		t.Fatalf("/proc/%d/stat: %q", pid, s)
	}
	ut, err1 := strconv.ParseUint(f[11], 10, 64)
	st, err2 := strconv.ParseUint(f[12], 10, 64)
	if err1 != nil || err2 != nil {
		t.Fatalf("/proc/%d/stat: %q", pid, s)
	}
	return time.Duration(ut+st) * statTickSlack
}

// checkGroupCPU holds pct, the CPU% the probe charged to a group made of a busy loop and its idle
// parent, to what those two used: busy and idle over outer, which holds the probe's sample (at least
// interval long, its CPU% taken over a span between interval and outer). A loop uses at most one second
// of CPU per second, so inside the sample it used at least busy-(outer-interval); the group used at most
// busy+idle. Each of the four readings (two here, two by the probe) is off by up to statTickSlack.
func checkGroupCPU(t *testing.T, pct float64, interval, outer, busy, idle time.Duration) {
	t.Helper()
	share := pct / 100
	low, high := time.Duration(float64(interval)*share), time.Duration(float64(outer)*share)
	most := busy + idle + 4*statTickSlack
	least := busy - (outer - interval) - 2*statTickSlack
	t.Logf("group %.1f%% = %v..%v of CPU; loop used %v over %v (sample %v): at least %v inside the sample, group at most %v",
		pct, low, high, busy, outer, interval, least, most)
	if low > most {
		t.Fatalf("the group is over-reported: %.1f%% over at least %v is %v of CPU, but the loop used %v and its parent %v", pct, interval, low, busy, idle)
	}
	// Without the loop the group is the idle parent alone: at most this much once scaled to outer.
	alone := time.Duration(float64(idle+2*statTickSlack) * float64(outer) / float64(interval))
	if least <= alone {
		t.Skipf("the scheduler gave the busy loop only %v of CPU in %v, so at least %v inside the %v sample: too little to tell it from its idle parent (%v)",
			busy, outer, least, interval, idle)
	}
	if high < least {
		t.Fatalf("the busy loop's CPU is missing from its lane: %.1f%% is at most %v of CPU, but the loop used at least %v inside the sample", pct, high, least)
	}
}
