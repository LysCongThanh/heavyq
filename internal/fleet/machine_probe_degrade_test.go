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

	"github.com/LysCongThanh/heavyq/internal/remote"
)

// namedCommand returns a script that does what target does ("true": exit at once; "sleep": wait the
// seconds given as its argument) under the command name name, so the test's processes are told apart
// from everything else: comm is the base name of the file a process was started from, and for a script
// that is the script, not its interpreter. A renamed link to the system's binary is not used: a
// multi-call coreutils (the Rust one some distributions ship) refuses to run under a name that is not
// its own and exits at once.
func namedCommand(t *testing.T, target, name string) string {
	t.Helper()
	bodies := map[string]string{
		"true":  "#!/bin/sh\nexit 0\n",
		"sleep": "#!/bin/bash\nread -r -t \"$1\" _ || :\n", // a builtin: no child; stdin is a pipe startMany keeps open
	}
	body, ok := bodies[target]
	if !ok {
		t.Fatalf("namedCommand: no script for %q", target)
	}
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not installed")
	}
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// procState is the state letter of pid in /proc/<pid>/stat ("" when it is gone).
func procState(pid int) string {
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return ""
	}
	s := string(b)
	if f := strings.Fields(s[strings.LastIndexByte(s, ')')+1:]); len(f) > 0 {
		return f[0]
	}
	return ""
}

// startMany starts n processes of path args, and waits until each is in state want (Z: exited and not
// reaped, since nothing waits for them before the cleanup; T: stopped).
func startMany(t *testing.T, n int, want string, path string, args ...string) []int {
	t.Helper()
	r, w, err := os.Pipe() // the sleepers' stdin: nothing is ever written, so their read waits
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close(); _ = w.Close() })
	var pids []int
	for range n {
		c := exec.Command(path, args...)
		c.Stdin = r
		if err := c.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = c.Process.Kill(); _ = c.Wait() })
		pids = append(pids, c.Process.Pid)
		if want == "T" {
			_ = syscall.Kill(c.Process.Pid, syscall.SIGSTOP)
		}
	}
	for _, pid := range pids {
		for deadline := time.Now().Add(10 * time.Second); procState(pid) != want; time.Sleep(5 * time.Millisecond) {
			if time.Now().After(deadline) {
				t.Fatalf("pid %d is %q, not %q", pid, procState(pid), want)
			}
		}
	}
	return pids
}

// groupOfComm sums the probe's groups of one command name.
func groupOfComm(groups []Group, comm string) (procs int, cpu float64) {
	for _, g := range groups {
		if g.Comm == comm {
			procs, cpu = procs+g.Procs, cpu+g.CPUPct
		}
	}
	return procs, cpu
}

// Zombies (exited, never reaped) use no CPU and no RAM: they are in no group, no count, no attribution.
// Stopped tasks still hold their RAM, so they stay in their group, at 0% CPU (stopped before the probe,
// they have no tick delta and so are never among the busiest: TestProbeNeverReadsTheCommandLineOfAStoppedTask
// checks one that is). Hundreds of each do not make the probe partial or slow: it stays within its budget.
func TestProbeLeavesZombiesOutAndStoppedTasksIn(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("zombies and stopped tasks are read from /proc")
	}
	zombies := startMany(t, 200, "Z", namedCommand(t, "true", "hq-zombie"))
	stopped := startMany(t, 20, "T", namedCommand(t, "sleep", "hq-stopped"), "60")
	timeout := 20 * time.Second
	start := time.Now()
	p, err := RunProbe(ProbeRequest{Transport: localTransport, Timeout: timeout, TopArgs: 100})
	took := time.Since(start)
	if err != nil {
		t.Fatal(err)
	}
	if p.Partial != "" || took > time.Duration(probeBudget(timeout))*time.Second {
		t.Fatalf("the probe was partial (%q) or slow (%s, budget %ds)", p.Partial, took, probeBudget(timeout))
	}
	groups, _ := Attribute(p, nil)
	if n, _ := groupOfComm(groups, "hq-zombie"); n != 0 {
		t.Fatalf("%d of %d zombies counted in a group", n, len(zombies))
	}
	for _, pr := range p.Procs {
		if pr.Pid == zombies[0] || pr.Comm == "hq-zombie" {
			t.Fatalf("a zombie is in the process table: %+v", pr)
		}
	}
	if n, cpu := groupOfComm(groups, "hq-stopped"); n != len(stopped) || cpu != 0 {
		t.Fatalf("stopped tasks: %d in their group at %.1f%% CPU, want %d at 0", n, cpu, len(stopped))
	}
}

// Hundreds of sleeping processes cost the probe a line each, nothing per process beyond its stat file:
// command lines are read for the busiest TopArgs only, the output stays compact, and the probe's own CPU
// time (the script and every process it started) stays small.
func TestProbeCostStaysBoundedWithManyProcesses(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the sample reads /proc")
	}
	const sleepers, topArgs = 300, 5
	startMany(t, sleepers, "S", namedCommand(t, "sleep", "hq-sleeper"), "60")
	timeout := 20 * time.Second
	cmd, err := remote.Transport(localTransport).Command(t.Context(), nil, fleetProbeScript, "-", strconv.Itoa(topArgs), strconv.Itoa(probeBudget(timeout)))
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	raw, err := cmd.Output()
	took := time.Since(start)
	if err != nil {
		t.Fatal(err)
	}
	p, err := ParseProbe(string(raw))
	if err != nil || p.Partial != "" {
		t.Fatalf("probe: %v, partial %q", err, p.Partial)
	}
	groups, _ := Attribute(p, nil)
	if n, _ := groupOfComm(groups, "hq-sleeper"); n != sleepers {
		t.Fatalf("%d sleepers in their group, want %d", n, sleepers)
	}
	if len(p.Args) > topArgs {
		t.Fatalf("%d command lines read, at most %d", len(p.Args), topArgs)
	}
	if perProc := len(raw) / len(p.Procs); perProc > 100 {
		t.Fatalf("%d bytes per process (%d bytes for %d processes)", perProc, len(raw), len(p.Procs))
	}
	cpu := cmd.ProcessState.UserTime() + cmd.ProcessState.SystemTime()
	if cpu > 2*time.Second || took > time.Duration(probeBudget(timeout))*time.Second {
		t.Fatalf("the probe of %d processes used %s CPU over %s", len(p.Procs), cpu, took)
	}
	t.Logf("%d processes: %d bytes, %s CPU, %s", len(p.Procs), len(raw), cpu, took)
}

// With a timeout too short for a CPU sample the script starts no expensive step: the machine's numbers
// arrive, the report says what is missing and why, and the machine is not unreachable.
func TestProbeWithATinyBudgetIsPartial(t *testing.T) {
	p, err := RunProbe(ProbeRequest{Transport: localTransport, Timeout: 1500 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	if p.Cores < 1 || p.TotalMB <= 0 || len(p.Procs) != 0 || !strings.Contains(p.Partial, "no top consumers and attribution (the probe's time budget was spent") {
		t.Fatalf("partial probe: cores %d total %d procs %d partial %q", p.Cores, p.TotalMB, len(p.Procs), p.Partial)
	}
}

// A machine that sent its numbers and then hung past the timeout keeps those numbers: the transport is
// ended at the timeout, and the probe is partial, not unreachable.
func TestProbeCutByTheTimeoutKeepsTheCheapPart(t *testing.T) {
	hang := []string{"sh", "-c", `printf 'heavyq-fleet 1\nos Linux\ncores 4\nload 9.5\nmem_total_mb 8000\nmem_avail_mb 100\ndone base\ndone slots\np 1 0 1 0 10 ini'; exec sleep 30`}
	start := time.Now()
	p, err := RunProbe(ProbeRequest{Transport: hang, Timeout: time.Second})
	if err != nil || time.Since(start) > 5*time.Second {
		t.Fatalf("err=%v after %s", err, time.Since(start))
	}
	if p.Cores != 4 || p.Load1 != 9.5 || p.FreeMB != 100 || len(p.Procs) != 0 || p.Partial != "no top consumers and attribution (timed out after 1s)" {
		t.Fatalf("cut probe: %+v", p)
	}
}
