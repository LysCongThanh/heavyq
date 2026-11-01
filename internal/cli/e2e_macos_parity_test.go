package cli

import (
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/LysCongThanh/heavyq/internal/procinfo"
)

// The tests in this file hold on every platform with a process source
// (/proc on Linux, ps on macOS): they are the macOS parity contract of
// issue #37, and the macOS CI job runs them.

// status --json on a Mac (or Linux) has measured metrics, CPU per lane and
// the outside-the-queue groups.
func TestStatusJSONHasMetricsAndCPUPerLane(t *testing.T) {
	if !procinfo.Default.Available() {
		t.Skip("no process source (/proc or ps) on this platform")
	}
	h := newRig(t, baseConfig)
	burn := h.start("5", "--lane", "burn", "--", "sh", "-c", "while :; do :; done")
	h.running(1)
	time.Sleep(200 * time.Millisecond) // let the loop accumulate CPU time before the first sample
	s := h.statusJSON()
	if len(s.Machines) != 1 {
		t.Fatalf("machines = %+v", s.Machines)
	}
	if m := s.Machines[0]; !m.MetricsKnown || m.Cores < 1 || m.TotalMB <= 0 || m.FreeMB <= 0 || m.FreeMB > m.TotalMB {
		t.Fatalf("local metrics must be measured: %+v", m)
	}
	if !s.CPU.Available || len(s.CPU.Outside) == 0 {
		t.Fatalf("cpu = %+v: want available, with outside groups", s.CPU)
	}
	var lane *float64
	for _, l := range s.CPU.Lanes {
		if l.Name == "burn" && l.Procs >= 1 {
			lane = &l.CPUPct
		}
	}
	if lane == nil {
		t.Fatalf("lane burn missing from cpu.lanes: %+v", s.CPU.Lanes)
	}
	if *lane < 10 {
		t.Fatalf("a spinning shell must show CPU in its lane, got %.1f%%", *lane)
	}
	_ = syscall.Kill(burn.cmd.Process.Pid, syscall.SIGTERM)
	burn.wait(t, 30*time.Second)
}

// A cancel reaches a grandchild that is still attached to its parent (the
// walk over the ppid map). Orphans that re-parented to init are Linux-only
// (see TestCancelReachesOrphansBeforeFreeingSlot): macOS has no subreaper.
func TestCancelReachesGrandchildren(t *testing.T) {
	if !procinfo.Default.Available() {
		t.Skip("no process source (/proc or ps) on this platform")
	}
	h := newRig(t, baseConfig)
	pf := filepath.Join(h.root, "grandchild.pid")
	// sh (child) -> sh -c (grandchild) -> sleep (great-grandchild); wait keeps every level alive
	job := h.start("5", "--lane", "tree", "--", "sh", "-c",
		`sh -c 'sleep 300 & echo $! > `+pf+`; wait' & wait`)
	h.running(1)
	h.until("grandchild started", func() bool { return strings.TrimSpace(h.read("grandchild.pid")) != "" })
	deep, err := strconv.Atoi(strings.TrimSpace(h.read("grandchild.pid")))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = syscall.Kill(deep, syscall.SIGKILL) })
	if rc, _ := h.run("cancel", strconv.Itoa(job.cmd.Process.Pid)); rc != 0 {
		t.Fatal("cancel failed")
	}
	if rc := job.wait(t, 30*time.Second); rc != ExitCancelled {
		t.Fatalf("rc=%d\n%s", rc, job.out)
	}
	h.until("great-grandchild gone", func() bool { return !procinfo.Default.Running(deep) })
}
