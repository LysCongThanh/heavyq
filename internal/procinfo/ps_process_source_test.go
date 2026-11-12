package procinfo

import (
	"os"
	"os/exec"
	"runtime"
	"syscall"
	"testing"
	"time"
)

// These tests drive the ps source against real processes. It is the
// production source on macOS and also works with procps on Linux, so the
// same checks run in both CI jobs; they skip where ps cannot list processes.
func psSource(t *testing.T) ProcFS {
	t.Helper()
	p := ProcFS{UsePS: true}
	if !p.Available() {
		t.Skip("ps cannot list processes here")
	}
	return p
}

func TestDefaultSourceMatchesPlatform(t *testing.T) {
	if want := runtime.GOOS == "darwin"; Default.UsePS != want {
		t.Fatalf("Default.UsePS = %v on %s", Default.UsePS, runtime.GOOS)
	}
}

func TestPSIdentity(t *testing.T) {
	p := psSource(t)
	st, err := p.StartTime(os.Getpid())
	if err != nil || st == "" {
		t.Fatalf("StartTime(self) = %q, %v", st, err)
	}
	if again, _ := p.StartTime(os.Getpid()); again != st {
		t.Fatalf("start token not stable: %q then %q", st, again)
	}
	if !p.Alive(os.Getpid(), st) || p.Alive(os.Getpid(), st+"x") {
		t.Fatal("Alive must match the token of this very process")
	}
	if _, err := p.StartTime(0); err == nil {
		t.Fatal("pid 0 is invalid")
	}
	cmd := exec.Command("true")
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	if _, err := p.StartTime(cmd.Process.Pid); err == nil {
		t.Fatal("reaped child must have no start token")
	}
	if !p.Running(os.Getpid()) || p.Running(cmd.Process.Pid) || p.Running(0) {
		t.Fatal("Running: self yes, reaped child and pid 0 no")
	}
}

func TestPSRunningIgnoresZombies(t *testing.T) {
	p := psSource(t)
	cmd := exec.Command("true")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Wait() }()
	// not waited for yet: once it exits it is a zombie, which is not running
	deadline := time.Now().Add(5 * time.Second)
	for p.Running(cmd.Process.Pid) {
		if time.Now().After(deadline) {
			t.Fatal("exited child still reported as running")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// The busy child is charged to the lane of its ancestor (this test process), with the CPU it really
// got: the witness is its own CPU time, read from its rusage at reap (see runBusyChild).
func TestPSBreakdownAttributesBusyChildToItsLane(t *testing.T) {
	p := psSource(t)
	var byRoot, outside []GroupUsage
	w := runBusyChild(t, "Breakdown", func(roots map[int]string) { byRoot, outside = p.Breakdown(roots, cpuWindow) })
	if len(byRoot) != 1 || byRoot[0].Group != "lane test" || byRoot[0].Procs < 2 || byRoot[0].RSSMB <= 0 || len(outside) == 0 {
		t.Fatalf("byRoot=%+v outside=%d groups", byRoot, len(outside))
	}
	if runtime.GOOS != "darwin" {
		// procps prints whole seconds: a 500 ms window cannot show CPU%. macOS ps has centiseconds.
		return
	}
	t.Run("Breakdown", func(t *testing.T) { w.check(t, byRoot[0].CPUPct) })
	t.Run("Attribute", func(t *testing.T) {
		var groups []GroupCPU
		w := runBusyChild(t, "Attribute", func(roots map[int]string) { groups = p.Attribute(roots, "outside", cpuWindow) })
		w.check(t, lanePercent(groups, "lane test"))
	})
}

func TestPSDescendantsReachGrandchildren(t *testing.T) {
	p := psSource(t)
	cmd := exec.Command("sh", "-c", "sh -c 'sleep 30' & sleep 30; wait")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	// sh (child) -> inner sh (grandchild) -> sleep, plus sleep: at least 2 below the child
	deadline := time.Now().Add(10 * time.Second)
	for len(p.Descendants(cmd.Process.Pid)) < 2 {
		if time.Now().After(deadline) {
			t.Fatalf("descendants = %v", p.Descendants(cmd.Process.Pid))
		}
		time.Sleep(50 * time.Millisecond)
	}
	for _, d := range p.Descendants(cmd.Process.Pid) {
		_ = syscall.Kill(d, syscall.SIGKILL)
	}
}
