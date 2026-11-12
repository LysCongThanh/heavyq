package procinfo

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"
)

// cpuWindow is the sampling interval the real-process CPU tests pass to the code under test.
const cpuWindow = 500 * time.Millisecond

// tickSlack is what one process's CPU delta can be off by in a sample: both sources read whole
// centiseconds (/proc clock ticks at USER_HZ 100, macOS ps cputime with two decimals).
const tickSlack = 10 * time.Millisecond

// cpuWitness is the ground truth around one sampling call: how much CPU the busy child really got,
// as the kernel reports it to its parent at reap, and not how much a test hoped the scheduler would
// give it. A loaded machine can starve the child; the bounds below hold anyway.
type cpuWitness struct {
	what     string        // the call under test, for messages
	interval time.Duration // the interval passed to the call: its sampling window is at least this long
	outer    time.Duration // from just before the child started until it was frozen: the window lies inside
	child    time.Duration // the child's whole CPU time (rusage at reap), frozen right after the call
	self     time.Duration // this test process's own CPU time over outer (it is the lane's root)
}

// runBusyChild starts a spinning shell as a child of this test process, runs sample (one call of the
// code under test, with this process as the lane's root) at once, then freezes the child (SIGSTOP,
// confirmed by wait4), kills and reaps it and returns its CPU time. Nothing waits for the child to
// "warm up": whatever share the scheduler grants it is what the witness measures.
func runBusyChild(t *testing.T, what string, sample func(roots map[int]string)) cpuWitness {
	t.Helper()
	self0 := selfCPU(t)
	start := time.Now()
	cmd := exec.Command("sh", "-c", "while :; do :; done")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	reaped := false
	t.Cleanup(func() {
		if !reaped {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	})
	sample(map[int]string{os.Getpid(): "lane test"})
	freeze(t, cmd.Process.Pid)
	w := cpuWitness{what: what, interval: cpuWindow, outer: time.Since(start), self: selfCPU(t) - self0}
	_ = cmd.Process.Kill()
	_ = cmd.Wait() // "signal: killed"
	reaped = true
	w.child = cmd.ProcessState.UserTime() + cmd.ProcessState.SystemTime()
	return w
}

// freeze stops pid and returns once the kernel reports it stopped: its CPU time no longer grows.
func freeze(t *testing.T, pid int) {
	t.Helper()
	if err := syscall.Kill(pid, syscall.SIGSTOP); err != nil {
		t.Fatal(err)
	}
	for {
		var ws syscall.WaitStatus
		_, err := syscall.Wait4(pid, &ws, syscall.WUNTRACED, nil)
		if errors.Is(err, syscall.EINTR) {
			continue
		}
		if err != nil || !stoppedBySIGSTOP(ws) {
			t.Fatalf("busy child did not stop: status %#x, %v", uint32(ws), err)
		}
		return
	}
}

// stoppedBySIGSTOP reads a wait status as both kernels write it: low byte 0x7f, then the signal.
// (Go's WaitStatus.Stopped is false for SIGSTOP on the BSDs and macOS, where it means "continued".)
func stoppedBySIGSTOP(ws syscall.WaitStatus) bool {
	return uint32(ws)&0xff == 0x7f && syscall.Signal(uint32(ws)>>8&0xff) == syscall.SIGSTOP
}

func selfCPU(t *testing.T) time.Duration {
	t.Helper()
	var ru syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &ru); err != nil {
		t.Fatal(err)
	}
	return time.Duration(ru.Utime.Nano() + ru.Stime.Nano())
}

// check holds lanePct (the CPU% the call charged to "lane test") to the witness.
//
// The call samples twice; the CPU% is the CPU used between the two reads over a span between
// interval (the sleep separates the reads) and outer, so lanePct/100*interval <= measured CPU time
// <= lanePct/100*outer. A single-threaded loop uses at most one second of CPU per second, so inside
// the window the child used at least child-(outer-interval); the lane (this process and the child)
// used at most child+self. Each delta is read in whole centiseconds (tickSlack per process).
func (w cpuWitness) check(t *testing.T, lanePct float64) {
	t.Helper()
	share := lanePct / 100
	low, high := scale(w.interval, share), scale(w.outer, share)
	most := w.child + w.self + 2*tickSlack
	if low > most {
		t.Fatalf("%s over-reports the lane: %.1f%% over at least %v is %v of CPU, but the busy child used %v in all and this process %v (%s)",
			w.what, lanePct, w.interval, low, w.child, w.self, w)
	}
	least := w.child - (w.outer - w.interval) - tickSlack
	t.Logf("%s: lane %.1f%% = %v..%v of CPU; child at least %v inside the window, lane at most %v (%s)", w.what, lanePct, low, high, least, most, w)
	// Without the child the lane is this process alone: at most self+tickSlack over a span of at least
	// interval, which reads as at most this much once scaled to outer. Below it, nothing tells them apart.
	alone := time.Duration(float64(w.self+tickSlack) * float64(w.outer) / float64(w.interval))
	if least <= alone {
		t.Skipf("%s: the scheduler gave the busy child only %v of CPU in %v, so at least %v inside the sampling window: "+
			"too little to tell it from this process's own %v (%s)", w.what, w.child, w.outer, least, w.self, w)
	}
	if high < least {
		t.Fatalf("%s under-reports the busy child in its ancestor's lane: %.1f%% is at most %v of CPU, but the child used at least %v inside the window (%s)",
			w.what, lanePct, high, least, w)
	}
}

func (w cpuWitness) String() string {
	return "interval " + w.interval.String() + ", outer " + w.outer.String() + ", child " + w.child.String() + ", self " + w.self.String()
}

func scale(d time.Duration, f float64) time.Duration { return time.Duration(float64(d) * f) }

// lanePercent is the Percent of group in groups, 0 when the group is missing.
func lanePercent(groups []GroupCPU, group string) float64 {
	for _, g := range groups {
		if g.Group == group {
			return g.Percent
		}
	}
	return 0
}
