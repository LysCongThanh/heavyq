package fleet

import (
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

// childCgroup creates a cgroup v2 below the test's own, or skips: it needs root or a delegated cgroup
// (a systemd user session delegates one; a CI runner usually does not).
func childCgroup(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("/proc/self/cgroup")
	own, ok := strings.CutPrefix(strings.TrimSpace(string(b)), "0::")
	if err != nil || !ok || strings.Contains(own, "\n") {
		t.Skip("no cgroup v2 unified hierarchy")
	}
	dir := filepath.Join("/sys/fs/cgroup", own, "hq-probe-frozen-"+strconv.Itoa(os.Getpid()))
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Skipf("cannot create a cgroup to freeze (needs root or a delegated cgroup): %v", err)
	}
	t.Cleanup(func() {
		for deadline := time.Now().Add(5 * time.Second); os.Remove(dir) != nil && time.Now().Before(deadline); {
			time.Sleep(20 * time.Millisecond) // the killed tasks leave the cgroup asynchronously
		}
	})
	return dir
}

// Tasks frozen by the cgroup freezer (a `docker pause`d container) are read from their stat file only:
// they stay in their group at 0% CPU, their command lines are not read, and the probe stays complete and
// within its budget.
func TestProbeOfFrozenTasksStaysComplete(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the cgroup freezer is Linux")
	}
	cg := childCgroup(t)
	frozen := startMany(t, 5, "S", namedCommand(t, "sleep", "hq-frozen"), "60")
	for _, pid := range frozen {
		if err := os.WriteFile(filepath.Join(cg, "cgroup.procs"), []byte(strconv.Itoa(pid)), 0o644); err != nil {
			t.Skipf("cannot move a task into the test cgroup (needs root or a delegated cgroup): %v", err)
		}
	}
	freeze := filepath.Join(cg, "cgroup.freeze")
	if err := os.WriteFile(freeze, []byte("1"), 0o644); err != nil {
		t.Skipf("cannot freeze the test cgroup: %v", err)
	}
	t.Cleanup(func() { _ = os.WriteFile(freeze, []byte("0"), 0o644) })
	for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		if b, _ := os.ReadFile(filepath.Join(cg, "cgroup.events")); strings.Contains(string(b), "frozen 1") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the test cgroup never froze")
		}
	}

	timeout := 20 * time.Second
	start := time.Now()
	p, err := RunProbe(ProbeRequest{Transport: localTransport, Timeout: timeout, TopArgs: 100})
	took := time.Since(start)
	if err != nil || p.Partial != "" || took > time.Duration(probeBudget(timeout))*time.Second {
		t.Fatalf("probe: err %v, partial %q, took %s", err, p.Partial, took)
	}
	groups, _ := Attribute(p, nil)
	if n, cpu := groupOfComm(groups, "hq-frozen"); n != len(frozen) || cpu != 0 {
		t.Fatalf("frozen tasks: %d in their group at %.1f%% CPU, want %d at 0", n, cpu, len(frozen))
	}
	for _, pid := range frozen {
		if _, ok := p.Args[pid]; ok {
			t.Fatalf("the command line of frozen pid %d was read", pid)
		}
	}
}
