package cli

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/LysCongThanh/heavyq/internal/queue"
)

func TestPriorityThenFIFO(t *testing.T) {
	h := newRig(t, baseConfig)
	hold := h.start("5", "--lane", "hold", "--", "sh", "-c", h.gateFile("go"))
	h.running(1)
	order := filepath.Join(h.root, "order")
	var ps []*proc
	for i, x := range [][2]string{{"7", "low"}, {"2", "high-a"}, {"2", "high-b"}, {"4", "mid"}} {
		ps = append(ps, h.start(x[0], "--lane", x[1], "--", "sh", "-c", "echo "+x[1]+" >> "+order))
		h.waiting(i + 1)
	}
	h.touch("go")
	for _, p := range append(ps, hold) {
		if rc := p.wait(t, 30*time.Second); rc != 0 {
			t.Fatalf("rc=%d: %s", rc, p.out)
		}
	}
	if got := strings.Fields(h.read("order")); strings.Join(got, ",") != "high-a,high-b,mid,low" {
		t.Fatalf("order = %v", got)
	}
}

func TestExitCodesAndUsage(t *testing.T) {
	h := newRig(t, baseConfig)
	if rc, _ := h.run("3", "--", "sh", "-c", "exit 42"); rc != 42 {
		t.Fatalf("command code not propagated: %d", rc)
	}
	for _, args := range [][]string{{"0", "--", "true"}, {"3", "true"}, {"3", "--bogus", "--", "true"}, {"3", "--"}, {"3", "--remote", "nowhere", "--", "true"}} {
		if rc, out := h.run(args...); rc != ExitUsage {
			t.Fatalf("%v: rc=%d want 2\n%s", args, rc, out)
		}
	}
	if rc, _ := h.run("3", "--", "definitely-not-a-command-xyz"); rc != 127 {
		t.Fatalf("missing command: rc=%d want 127", rc)
	}
}

func TestMaxWaitAndHeavyNeverUsesLightSlot(t *testing.T) {
	h := newRig(t, baseConfig) // 1 heavy + 1 light slot
	hold := h.start("5", "--", "sh", "-c", h.gateFile("go"))
	h.running(1)
	start := time.Now()
	rc, out := h.run("1", "--max-wait", "400ms", "--", "true")
	if rc != ExitMaxWait || time.Since(start) > 10*time.Second {
		t.Fatalf("rc=%d after %s, want 75 (the light slot is free but this is a heavy job)\n%s", rc, time.Since(start), out)
	}
	if s := h.snapshot(); len(s.Waiting) != 0 {
		t.Fatal("ticket left behind after max-wait")
	}
	h.touch("go")
	hold.wait(t, 20*time.Second)
}

func TestLightJumpsIntoLightSlot(t *testing.T) {
	h := newRig(t, baseConfig)
	hold := h.start("5", "--lane", "hold", "--", "sh", "-c", h.gateFile("go"))
	h.running(1)
	heavy := h.start("1", "--lane", "heavy", "--", "true")
	h.waiting(1)
	rc, out := h.run("9", "--light", "--lane", "lint", "--", "echo", "linted")
	if rc != 0 || !strings.Contains(out, "local/light-1") || !strings.Contains(out, "linted") {
		t.Fatalf("light ticket should backfill the light slot: rc=%d\n%s", rc, out)
	}
	if s := h.snapshot(); len(s.Waiting) != 1 || s.Waiting[0].Lane != "heavy" {
		t.Fatal("the heavy ticket must still be waiting for a heavy slot")
	}
	h.touch("go")
	for _, p := range []*proc{hold, heavy} {
		if rc := p.wait(t, 20*time.Second); rc != 0 {
			t.Fatalf("rc=%d", rc)
		}
	}
}

func TestLegacyFlockUsersShareSlots(t *testing.T) {
	legacy := filepath.Join(t.TempDir(), "legacy.lock")
	h := newRig(t, baseConfig+"legacy_lock_files = [\""+legacy+"\"]\n")
	flock := newExternal(t, h, "flock", legacy, "sh", "-c", h.gateFile("go"))
	h.until("flock(1) holds the legacy file", func() bool { return isLocked(legacy) })
	if rc, out := h.run("1", "--max-wait", "300ms", "--", "true"); rc != ExitMaxWait {
		t.Fatalf("legacy holder must block heavyq: rc=%d\n%s", rc, out)
	}
	h.touch("go")
	flock.wait(t, 20*time.Second)
	if rc, out := h.run("1", "--max-wait", "10s", "--", "true"); rc != 0 || !strings.Contains(out, "local/legacy") {
		t.Fatalf("rc=%d\n%s", rc, out)
	}
}

// A daemon started by the job must not keep the slot after heavyq exits.
func TestDaemonLeftByJobDoesNotPinSlot(t *testing.T) {
	h := newRig(t, baseConfig)
	pidFile := filepath.Join(h.root, "daemon.pid")
	if rc, out := h.run("3", "--", "sh", "-c", "sleep 30 >/dev/null 2>&1 & echo $! > "+pidFile); rc != 0 {
		t.Fatalf("rc=%d\n%s", rc, out)
	}
	t.Cleanup(func() { killPidFile(pidFile) })
	if rc, out := h.run("3", "--max-wait", "2s", "--", "true"); rc != 0 {
		t.Fatalf("slot still pinned by the daemon: rc=%d\n%s", rc, out)
	}
}

func TestSignalsCleanUpTicketsAndChildren(t *testing.T) {
	h := newRig(t, baseConfig)
	pidFile := filepath.Join(h.root, "child.pid")
	running := h.start("5", "--lane", "busy", "--", "sh", "-c", "echo $$ > "+pidFile+"; exec sleep 60")
	h.running(1)
	waiter := h.start("5", "--lane", "waiter", "--", "true")
	h.waiting(1)
	_ = waiter.cmd.Process.Signal(syscall.SIGTERM)
	if rc := waiter.wait(t, 10*time.Second); rc != 143 {
		t.Fatalf("waiting ticket: rc=%d want 143", rc)
	}
	h.until("pid file", func() bool { return h.read("child.pid") != "" })
	_ = running.cmd.Process.Signal(syscall.SIGTERM)
	if rc := running.wait(t, 10*time.Second); rc != 143 {
		t.Fatalf("running ticket: rc=%d want 143", rc)
	}
	if s := h.snapshot(); len(s.Waiting)+len(s.Running) != 0 {
		t.Fatalf("tickets left: %+v", s)
	}
	pid := atoiTrim(h.read("child.pid"))
	if pid > 0 && syscall.Kill(pid, 0) == nil {
		t.Fatalf("child %d still alive after heavyq was terminated", pid)
	}
	if hist := h.read("history.jsonl"); !strings.Contains(hist, `"exit":143`) {
		t.Fatalf("interrupted run not in history: %s", hist)
	}
}

func TestDeadTicketIsPruned(t *testing.T) {
	h := newRig(t, baseConfig)
	d, _ := queue.Open(filepath.Join(h.root, "q"))
	ghost := &queue.Ticket{Pid: os.Getpid(), Start: "recycled", Prio: 1, Lane: "ghost", Candidates: []string{"local"}}
	if err := d.Add(ghost); err != nil {
		t.Fatal(err)
	}
	if rc, out := h.run("9", "--max-wait", "5s", "--", "true"); rc != 0 {
		t.Fatalf("dead ticket must not block: rc=%d\n%s", rc, out)
	}
	if d.Exists(ghost) {
		t.Fatal("dead ticket not pruned")
	}
}
