package procinfo

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestStartTimeAndAlive(t *testing.T) {
	st, err := Default.StartTime(os.Getpid())
	if err != nil || st == "" {
		t.Fatalf("StartTime(self) = %q, %v", st, err)
	}
	if !Default.Alive(os.Getpid(), st) {
		t.Fatal("self must be alive")
	}
	if Default.Alive(os.Getpid(), st+"0") {
		t.Fatal("a different start token means a recycled pid")
	}
	cmd := exec.Command("true")
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	if Default.Alive(cmd.Process.Pid, st) {
		t.Fatal("reaped child must not be alive")
	}
}

// writeStat creates a fake /proc/<pid>/stat whose comm contains spaces and
// parentheses, which the parser must survive.
func writeStat(t *testing.T, root string, pid, ppid int, ticks uint64) {
	t.Helper()
	dir := filepath.Join(root, fmt.Sprint(pid))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// fields after comm: state ppid pgrp session tty tpgid flags minflt cminflt majflt cmajflt utime stime ... starttime(22)
	line := fmt.Sprintf("%d (a b) (c)) S %d 1 1 0 -1 0 0 0 0 0 %d 0 0 0 20 0 1 0 777 0 0\n", pid, ppid, ticks)
	if err := os.WriteFile(filepath.Join(dir, "stat"), []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestAttributeWalksAncestry(t *testing.T) {
	root := t.TempDir()
	p := ProcFS{Root: root}
	// tree: 10 (ticket api) -> 11 -> 12 ; 20 (ticket web) ; 30 orphan
	a := map[int]procSample{10: {ppid: 1}, 11: {ppid: 10}, 12: {ppid: 11}, 20: {ppid: 1}, 30: {ppid: 1}}
	b := map[int]procSample{10: {ppid: 1, ticks: 10}, 11: {ppid: 10, ticks: 20}, 12: {ppid: 11, ticks: 30},
		20: {ppid: 1, ticks: 50}, 30: {ppid: 1, ticks: 5}}
	got := attribute(a, b, map[int]string{10: "lane api", 20: "lane web"}, "outside", time.Second)
	want := map[string]float64{"lane api": 60, "lane web": 50, "outside": 5}
	if len(got) != 3 || got[0].Group != "lane api" {
		t.Fatalf("got %+v", got)
	}
	for _, g := range got {
		if g.Percent != want[g.Group] {
			t.Errorf("%s = %.1f, want %.1f", g.Group, g.Percent, want[g.Group])
		}
	}
	// parser: fake tree with tricky comm
	writeStat(t, root, 42, 7, 99)
	f, err := p.statFields(42)
	if err != nil || f[1] != "7" || f[11] != "99" || f[19] != "777" {
		t.Fatalf("statFields = %v, %v", f, err)
	}
}

func TestAttributeRealBusyChild(t *testing.T) {
	if !Default.Available() {
		t.Skip("no /proc")
	}
	cmd := exec.Command("sh", "-c", "while :; do :; done")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	time.Sleep(100 * time.Millisecond)
	groups := Default.Attribute(map[int]string{os.Getpid(): "lane test"}, "outside", 300*time.Millisecond)
	for _, g := range groups {
		if g.Group == "lane test" && g.Percent > 5 {
			return
		}
	}
	t.Fatalf("busy child not attributed to its ancestor's lane: %+v", groups)
}

func TestBreakdownGroupsOutsideByCommand(t *testing.T) {
	a := map[int]procSample{10: {ppid: 1}, 11: {ppid: 10}, 30: {ppid: 1}, 31: {ppid: 1}}
	b := map[int]procSample{
		10: {ppid: 1, ticks: 10, comm: "heavyq", rssKB: 1024},
		11: {ppid: 10, ticks: 90, comm: "go", rssKB: 4096},
		30: {ppid: 1, ticks: 20, comm: "claude", rssKB: 2048},
		31: {ppid: 1, ticks: 20, comm: "claude", rssKB: 2048},
		40: {ppid: 2, ticks: 99, comm: "kworker/0:1"},
	}
	byRoot, outside := breakdown(a, b, map[int]string{10: "ticket-a"}, time.Second)
	if len(byRoot) != 1 || byRoot[0] != (GroupUsage{Group: "ticket-a", Procs: 2, CPUPct: 100, RSSMB: 5}) {
		t.Fatalf("byRoot = %+v", byRoot)
	}
	if len(outside) != 1 || outside[0] != (GroupUsage{Group: "claude", Procs: 2, CPUPct: 40, RSSMB: 4}) {
		t.Fatalf("outside = %+v (kernel threads must be ignored)", outside)
	}
}

func TestBreakdownReal(t *testing.T) {
	if !Default.Available() {
		t.Skip("no /proc")
	}
	byRoot, outside := Default.Breakdown(map[int]string{os.Getpid(): "self"}, 50*time.Millisecond)
	if len(byRoot) != 1 || byRoot[0].RSSMB <= 0 || len(outside) == 0 {
		t.Fatalf("byRoot=%+v outside=%d groups", byRoot, len(outside))
	}
	if Default.UsePS {
		return // comm of the /proc stat file only exists on the /proc source
	}
	if comm, _, err := Default.stat(os.Getpid()); err != nil || comm == "" {
		t.Fatalf("comm of self = %q %v", comm, err)
	}
}

func TestDescendantsReal(t *testing.T) {
	if !Default.Available() {
		t.Skip("no /proc")
	}
	cmd := exec.Command("sh", "-c", "sleep 30 & sleep 30; wait")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	deadline := time.Now().Add(5 * time.Second)
	for len(Default.Descendants(cmd.Process.Pid)) < 2 {
		if time.Now().After(deadline) {
			t.Fatalf("descendants = %v", Default.Descendants(cmd.Process.Pid))
		}
		time.Sleep(20 * time.Millisecond)
	}
	for _, d := range Default.Descendants(cmd.Process.Pid) {
		_ = syscall.Kill(d, syscall.SIGKILL)
	}
}
