package procinfo

import (
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"
)

// surveySources are the process sources this machine can drive: /proc where it
// exists, and ps wherever ps can list processes (macOS, procps on Linux).
func surveySources(t *testing.T) map[string]ProcFS {
	t.Helper()
	out := map[string]ProcFS{}
	if p := (ProcFS{Root: "/proc"}); p.Available() {
		out["proc"] = p
	}
	if p := (ProcFS{UsePS: true}); p.Available() {
		out["ps"] = p
	}
	if len(out) == 0 {
		t.Skip("no process source here")
	}
	return out
}

// stoppedChild starts a sleep and stops it with SIGSTOP, waiting until the
// kernel reports it stopped (job-control state, as Ctrl-Z leaves a job).
func stoppedChild(t *testing.T, p ProcFS) int {
	t.Helper()
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	if err := cmd.Process.Signal(syscall.SIGSTOP); err != nil {
		t.Fatal(err)
	}
	pid := cmd.Process.Pid
	deadline := time.Now().Add(10 * time.Second)
	for {
		got, ok := p.Survey([]int{pid})
		if ok && got[pid].Stopped {
			return pid
		}
		if time.Now().After(deadline) {
			t.Fatalf("child %d never reported stopped: %+v ok=%v", pid, got, ok)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestSurveyReportsLiveDeadAndStoppedProcesses(t *testing.T) {
	for name, p := range surveySources(t) {
		t.Run(name, func(t *testing.T) {
			self := os.Getpid()
			start, err := p.StartTime(self)
			if err != nil {
				t.Fatal(err)
			}
			dead := exec.Command("true")
			if err := dead.Run(); err != nil {
				t.Fatal(err)
			}
			stopped := stoppedChild(t, p)
			stoppedStart, err := p.StartTime(stopped)
			if err != nil {
				t.Fatal(err)
			}
			got, ok := p.Survey([]int{self, dead.Process.Pid, stopped, 0, -4})
			if !ok {
				t.Fatal("survey failed on a healthy process table")
			}
			if o, live := got[self]; !live || o.Start != start || o.Stopped {
				t.Fatalf("self: %+v (live %v), want start %q, running", o, live, start)
			}
			if _, live := got[dead.Process.Pid]; live {
				t.Fatalf("a reaped child is not alive: %+v", got)
			}
			if o := got[stopped]; !o.Stopped || o.Start != stoppedStart {
				t.Fatalf("stopped child: %+v, want stopped with start %q", o, stoppedStart)
			}
			if len(got) != 2 {
				t.Fatalf("survey = %+v, want exactly self and the stopped child", got)
			}
			if none, ok := p.Survey(nil); !ok || len(none) != 0 {
				t.Fatalf("empty survey = %+v, %v", none, ok)
			}
		})
	}
}
