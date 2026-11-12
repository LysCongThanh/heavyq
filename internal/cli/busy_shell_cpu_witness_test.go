package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// cpuTickSlack is what one CPU-time reading can be off by: /proc, ps and the shell's `times` all
// read whole centiseconds or finer.
const cpuTickSlack = 10 * time.Millisecond

// idleRunnerCPU is the most CPU the `heavyq run` process, which is in the lane too and only waits on
// its child, is allowed in one sampling window.
const idleRunnerCPU = 50 * time.Millisecond

// busyShell is a spinning shell queued by a test. It reports its own CPU time (the shell's `times`
// builtin, from the kernel) when sent SIGUSR1: a ground truth that neither heavyq nor ps computes.
type busyShell struct {
	t      *testing.T
	pid    int
	report string // file the shell appends its `times` output to, two lines per reading
	reads  int
}

// cpuSample is one reading of the shell's CPU time, taken at some instant between sent and seen.
type cpuSample struct {
	sent, seen time.Time
	cpu        time.Duration
}

// busyShellScript is the command line of the shell: the trap is set before the pid is published, so
// a signal never meets the default action of SIGUSR1 (terminate).
func (h *rig) busyShellScript(name string) string {
	return fmt.Sprintf(`trap 'times >> %q' USR1; echo $$ > %q; while :; do :; done`,
		filepath.Join(h.root, name+".times"), filepath.Join(h.root, name+".pid"))
}

// busyShellOf waits until the shell started by busyShellScript(name) publishes its pid.
func (h *rig) busyShellOf(name string) *busyShell {
	h.t.Helper()
	h.until(name+" pid", func() bool { return strings.HasSuffix(h.read(name+".pid"), "\n") })
	pid, err := strconv.Atoi(strings.TrimSpace(h.read(name + ".pid")))
	if err != nil {
		h.t.Fatal(err)
	}
	return &busyShell{t: h.t, pid: pid, report: filepath.Join(h.root, name+".times")}
}

var timesField = regexp.MustCompile(`(\d+)m([0-9.]+)s`)

// sample asks the shell for its CPU time (user+system of the shell itself: the first line of `times`)
// and waits for the answer.
func (b *busyShell) sample() cpuSample {
	b.t.Helper()
	s := cpuSample{sent: time.Now()}
	if err := syscall.Kill(b.pid, syscall.SIGUSR1); err != nil {
		b.t.Fatal(err)
	}
	b.reads++
	var lines []string
	for deadline := s.sent.Add(20 * time.Second); ; time.Sleep(2 * time.Millisecond) {
		out, _ := os.ReadFile(b.report)
		if lines = strings.Split(string(out), "\n"); len(lines) > 2*b.reads { // the last reading complete
			break
		}
		if time.Now().After(deadline) {
			b.t.Fatalf("the busy shell did not report its times: %q", out)
		}
	}
	s.seen = time.Now()
	line := lines[2*(b.reads-1)]
	f := timesField.FindAllStringSubmatch(line, -1)
	if len(f) != 2 {
		b.t.Fatalf("times printed %q", line)
	}
	for _, m := range f {
		mins, _ := strconv.Atoi(m[1])
		secs, err := strconv.ParseFloat(m[2], 64)
		if err != nil {
			b.t.Fatalf("times printed %q", line)
		}
		s.cpu += time.Duration(mins)*time.Minute + time.Duration(secs*float64(time.Second))
	}
	return s
}

// leastInAnyWindow is the least CPU the shell can have used in any window of length l inside [from,
// to], given readings taken one after the other before, during and after it. In a window [x, x+l],
// readings i < j sent at or after x and seen by x+l were both taken inside it, so the shell used at
// least cpu(j)-cpu(i) there. For x between two sends the worst is x just after the earlier one (or
// from itself): those are the only starts to try.
func leastInAnyWindow(samples []cpuSample, from, to time.Time, l time.Duration) time.Duration {
	type start struct {
		x      time.Time
		strict bool // x stands for "just after x": a reading sent at x is not inside
	}
	starts := []start{{from, false}}
	for _, s := range samples {
		if !s.sent.Before(from) {
			starts = append(starts, start{s.sent, true})
		}
	}
	least, tried := time.Duration(0), false
	for _, st := range starts {
		end := st.x.Add(l)
		if end.After(to) {
			continue
		}
		i, j := -1, -1
		for k, s := range samples {
			if i < 0 && (s.sent.After(st.x) || !st.strict && s.sent.Equal(st.x)) {
				i = k
			}
			if !s.seen.After(end) {
				j = k
			}
		}
		got := time.Duration(0)
		if i >= 0 && j > i {
			got = samples[j].cpu - samples[i].cpu - cpuTickSlack // each reading is whole ticks, or finer
		}
		if !tried || got < least {
			least, tried = got, true
		}
	}
	return least
}

// checkLaneCPU holds lanePct, the CPU% status charged to the shell's lane over a sampling window of
// at least interval inside [from, to], to the shell's readings: inside any such window it used at
// least leastInAnyWindow; over [from, to] at most the difference of the readings taken before and
// after. The lane is that shell plus the idle `heavyq run`. Each tick count is off by up to cpuTickSlack.
func checkLaneCPU(t *testing.T, lanePct float64, interval time.Duration, from, to time.Time, samples []cpuSample) {
	t.Helper()
	share, outer := lanePct/100, to.Sub(from)
	low, high := time.Duration(float64(interval)*share), time.Duration(float64(outer)*share)
	used := samples[len(samples)-1].cpu - samples[0].cpu
	most := used + cpuTickSlack + idleRunnerCPU + 2*cpuTickSlack
	least := leastInAnyWindow(samples, from, to, interval)
	t.Logf("lane %.1f%% = %v..%v of CPU; shell used %v over %v (%d readings, interval %v): at least %v inside any window, lane at most %v",
		lanePct, low, high, used, outer, len(samples), interval, least, most)
	if low > most {
		t.Fatalf("the lane is over-reported: %.1f%% over at least %v is %v of CPU, but the busy shell used %v in %v", lanePct, interval, low, used, outer)
	}
	if least <= 2*cpuTickSlack {
		t.Skipf("the scheduler starved the busy shell: %v of CPU in %v, at least %v inside a %v window; nothing to check", used, outer, least, interval)
	}
	if high < least-cpuTickSlack {
		t.Fatalf("a spinning shell must show its CPU in its lane: %.1f%% is at most %v of CPU, but the shell used at least %v inside the window",
			lanePct, high, least)
	}
}

// The window bound only counts readings taken inside the window, whatever the window's position.
func TestLeastInAnyWindow(t *testing.T) {
	t0 := time.Unix(1000, 0)
	at := func(ms int) time.Time { return t0.Add(time.Duration(ms) * time.Millisecond) }
	// a reading every 100 ms, answered after 10 ms, the shell at full speed
	var full []cpuSample
	for ms := 0; ms <= 1000; ms += 100 {
		full = append(full, cpuSample{sent: at(ms), seen: at(ms + 10), cpu: time.Duration(ms) * time.Millisecond})
	}
	// worst 500 ms window in [0, 1000]: starting just after a send, it holds 4 readings 300 ms apart
	if got := leastInAnyWindow(full, at(0), at(1000), 500*time.Millisecond); got != 300*time.Millisecond-cpuTickSlack {
		t.Fatalf("full speed: %v", got)
	}
	// no reading inside a window: nothing is known
	sparse := []cpuSample{full[0], full[10]}
	if got := leastInAnyWindow(sparse, at(0), at(1000), 500*time.Millisecond); got != 0 {
		t.Fatalf("sparse: %v", got)
	}
	// a stalled shell (no CPU between readings) is bounded by zero, minus a tick
	stalled := []cpuSample{full[0], {sent: at(200), seen: at(210)}, {sent: at(400), seen: at(410)}, {sent: at(900), seen: at(910)}}
	if got := leastInAnyWindow(stalled, at(0), at(1000), 500*time.Millisecond); got > 0 {
		t.Fatalf("stalled: %v", got)
	}
}
