package fleet

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// localTransport is the remote test rig: `sh -c` makes the "remote" this machine, no ssh needed.
var localTransport = []string{"sh", "-c"}

// The probe runs the real script on a real machine (this one) and reads real processes: a busy loop
// started under a ticket's process is attributed to that ticket's lane, and a slot lock held by a real
// flock(1) process is busy, with its holder.
func TestProbeAttributesRealProcesses(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("slot holders and cwd come from /proc")
	}
	if _, err := exec.LookPath("flock"); err != nil {
		t.Skip("flock(1) not installed")
	}
	slots := t.TempDir()
	work := t.TempDir()
	wt := filepath.Join(work, "wt", "app-1.d", "app")
	if err := os.MkdirAll(wt, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{"s1", "s2"} {
		if err := os.WriteFile(filepath.Join(slots, s+".lock"), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// a ticket's own process with a CPU-bound child (bounded by timeout(1) should the cleanup not run; --foreground keeps it in our process group)
	busyPid := filepath.Join(t.TempDir(), "busy.pid")
	owner := exec.Command("sh", "-c", "timeout --foreground 30 sh -c 'echo $$ > \""+busyPid+"\"; while :; do :; done' & wait")
	// a run that holds slot s2 the way `flock <file> cmd` does
	holder := exec.Command("flock", filepath.Join(slots, "s2.lock"), "sleep", "30")
	// a process working in a heavyq remote worktree
	inWT := exec.Command("sleep", "30")
	inWT.Dir = wt
	for _, c := range []*exec.Cmd{owner, holder, inWT} {
		c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} // the cleanup kills the whole group, children included
		if err := c.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = syscall.Kill(-c.Process.Pid, syscall.SIGKILL); _ = c.Wait() })
	}
	time.Sleep(300 * time.Millisecond) // let flock take the lock
	loop := 0
	for deadline := time.Now().Add(10 * time.Second); loop == 0; time.Sleep(20 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("the busy loop never published its pid")
		}
		if b, err := os.ReadFile(busyPid); err == nil && strings.HasSuffix(string(b), "\n") {
			loop, _ = strconv.Atoi(strings.TrimSpace(string(b)))
		}
	}

	busy0, owner0, t0 := statCPU(t, loop), statCPU(t, owner.Process.Pid), time.Now()
	p, err := RunProbe(ProbeRequest{Transport: localTransport, WorkDir: work, Slots: []SlotDir{{Owner: "bashq", Dir: slots}},
		Timeout: 20 * time.Second, TopArgs: 8})
	if err != nil {
		t.Fatal(err)
	}
	outer, busyUsed, ownerUsed := time.Since(t0), statCPU(t, loop)-busy0, statCPU(t, owner.Process.Pid)-owner0
	if p.Cores < 1 || p.TotalMB <= 0 || p.FreeMB <= 0 || len(p.Procs) < 3 || p.WorkDir != work {
		t.Fatalf("metrics: %+v", p)
	}
	busy := map[string]SlotState{}
	for _, s := range p.Slots {
		busy[s.Name] = s
	}
	if busy["s1"].Busy || !busy["s2"].Busy || busy["s2"].Holder != holder.Process.Pid || busy["s2"].Owner != "bashq" {
		t.Fatalf("slots: %+v (holder %d)", p.Slots, holder.Process.Pid)
	}

	roots := []Root{{Pid: owner.Process.Pid, Owner: "bashq", Lane: "api"}, {Pid: holder.Process.Pid, Owner: "bashq", Lane: "web"}}
	groups, outside := Attribute(p, roots)
	find := func(owner, lane, comm string) *Group {
		for i := range groups {
			if groups[i].Owner == owner && groups[i].Lane == lane && groups[i].Comm == comm {
				return &groups[i]
			}
		}
		return nil
	}
	api := find("bashq", "api", "sh") // the owner sh and the busy sh; timeout(1) is a group of its own
	if api == nil || api.Procs < 2 {
		t.Fatalf("the busy loop belongs to lane api: %+v", groups)
	}
	if g := find("bashq", "web", "sleep"); g == nil {
		t.Fatalf("the slot holder's child belongs to lane web: %+v", groups)
	}
	if g := find(OwnerHeavyq, "", "sleep"); g == nil {
		t.Fatalf("a process in <work_dir>/wt belongs to heavyq: %+v", groups)
	}
	if outside <= 0 {
		t.Fatalf("processes outside every queue use RAM too: %d", outside)
	}
	for i := 1; i < len(groups); i++ {
		if groups[i].CPUPct > groups[i-1].CPUPct {
			t.Fatalf("groups are sorted by CPU: %+v", groups)
		}
	}
	// last: it may skip when the machine starved the loop
	checkGroupCPU(t, api.CPUPct, time.Second, outer, busyUsed, ownerUsed)
}

// A dead or hanging machine shows as unreachable after its timeout; it never holds the report.
func TestProbeOfAHangingMachineTimesOut(t *testing.T) {
	start := time.Now()
	_, err := RunProbe(ProbeRequest{Transport: []string{"sh", "-c", "sleep 30"}, Timeout: 500 * time.Millisecond})
	if err == nil || time.Since(start) > 5*time.Second {
		t.Fatalf("err=%v after %s", err, time.Since(start))
	}
}

func TestParseProbeOutput(t *testing.T) {
	out := strings.Join([]string{
		"noise from a login script",
		"heavyq-fleet 1",
		"os Linux",
		"mem_total_mb 32000",
		"mem_avail_mb 1200",
		"load 45.10",
		"cores 18",
		"psi_cpu some avg10=61.50 avg60=50.00 avg300=40.00 total=1",
		"psi_memory some avg10=3.00 avg60=1.00 avg300=0.50 total=1",
		"psi_memory full avg10=1.25 avg60=0.10 avg300=0.00 total=1",
		"p 10 1 64 820.5 4096000 node",
		"p 11 10 12 99.0 512000 Web Content",
		"self 99",                                                     // the probe itself and its ps are not the machine's load
		"clk 100", "interval_ns 2000000000", "c 10 400", "c 100 1800", // 400 ticks over 2 s at 100 Hz: 200%
		"p 99 1 1 500.0 1000 bash",
		"p 100 99 1 900.0 1000 ps",
		"a\t10\tnode jest --maxWorkers=18 --token=supersecret",
		"w 11",
		"s bashq s1 1 10",
		"s bashq s2 0 -",
	}, "\n")
	p, err := ParseProbe(out)
	if err != nil {
		t.Fatal(err)
	}
	if p.Cores != 18 || p.Load1 != 45.1 || p.FreeMB != 1200 || p.TotalMB != 32000 || p.Pressure == nil ||
		p.Pressure.CPUSomeAvg10 != 61.5 || p.Pressure.MemFullAvg10 != 1.25 || p.Pressure.MemSomeAvg10 != 3 {
		t.Fatalf("metrics: %+v %+v", p, p.Pressure)
	}
	if len(p.Procs) != 2 || p.Procs[1].Comm != "Web Content" || p.Procs[0].Threads != 64 || p.Procs[0].RSSKB != 4096000 {
		t.Fatalf("procs: %+v", p.Procs)
	}
	if !strings.Contains(p.Args[10], "jest") || !p.InWorkTree[11] {
		t.Fatalf("args: %+v, in work tree: %v", p.Args, p.InWorkTree)
	}
	if len(p.Slots) != 2 || !p.Slots[0].Busy || p.Slots[0].Holder != 10 || p.Slots[1].Busy {
		t.Fatalf("slots: %+v", p.Slots)
	}
	groups, _ := Attribute(p, []Root{{Pid: 10, Owner: "bashq", Lane: "api"}})
	if len(groups) != 2 || groups[0].Owner != "bashq" || groups[0].CPUPct != 200 || groups[1].CPUPct != 0 || strings.Contains(groups[0].Cmd, "supersecret") {
		t.Fatalf("groups: %+v", groups)
	}
	if _, err := ParseProbe("Permission denied (publickey)."); err == nil {
		t.Fatal("output without the marker is an error")
	}
}
