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

func TestPSBreakdownAttributesBusyChildToItsLane(t *testing.T) {
	p := psSource(t)
	cmd := exec.Command("sh", "-c", "while :; do :; done")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	time.Sleep(100 * time.Millisecond)
	roots := map[int]string{os.Getpid(): "lane test"}
	byRoot, outside := p.Breakdown(roots, 500*time.Millisecond)
	if len(byRoot) != 1 || byRoot[0].Procs < 2 || byRoot[0].RSSMB <= 0 || len(outside) == 0 {
		t.Fatalf("byRoot=%+v outside=%d groups", byRoot, len(outside))
	}
	if runtime.GOOS != "darwin" {
		// procps prints whole seconds: a 500 ms window cannot show CPU%. macOS ps has centiseconds.
		return
	}
	if byRoot[0].CPUPct < 20 {
		t.Fatalf("busy child under-reported: %+v", byRoot[0])
	}
	groups := p.Attribute(roots, "outside", 500*time.Millisecond)
	for _, g := range groups {
		if g.Group == "lane test" && g.Percent > 20 {
			return
		}
	}
	t.Fatalf("busy child not attributed to its ancestor's lane: %+v", groups)
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
