package queue

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/LysCongThanh/heavyq/internal/procinfo"
)

// holdMutex takes the queue mutex through its own open file description, the
// way another heavyq process holds it (flock locks belong to the open file).
func holdMutex(t *testing.T, d *Dir) func() {
	t.Helper()
	f, err := os.OpenFile(filepath.Join(d.Path, ".mutex"), os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	return func() { _ = f.Close() }
}

// A holder that stopped (Ctrl-Z) or hangs keeps the mutex: a waiter must get
// its turn back after the wait, and at once when its caller wants to stop.
func TestMutexGivesUpOnAHolderThatNeverReleases(t *testing.T) {
	d, _ := Open(t.TempDir())
	release := holdMutex(t, d)
	defer release()
	start := time.Now()
	if _, err := d.Mutex(200*time.Millisecond, nil); !errors.Is(err, ErrMutexBusy) {
		t.Fatalf("err = %v, want ErrMutexBusy", err)
	}
	if took := time.Since(start); took < 150*time.Millisecond || took > 5*time.Second {
		t.Fatalf("gave up after %s, want about the 200ms wait", took)
	}
	start = time.Now()
	if _, err := d.Mutex(time.Minute, func() bool { return true }); !errors.Is(err, ErrMutexBusy) {
		t.Fatalf("err = %v, want ErrMutexBusy", err)
	}
	if took := time.Since(start); took > 2*time.Second {
		t.Fatalf("a caller that wants to stop waited %s", took)
	}
	release()
	unlock, err := d.Mutex(5*time.Second, nil)
	if err != nil {
		t.Fatalf("free mutex not taken: %v", err)
	}
	unlock()
}

// The file name is the queue order: a wall clock stepped back (NTP, a VM
// resume, a manual date) must not put new tickets before older ones.
func TestAddKeepsFIFOWhenTheClockStepsBack(t *testing.T) {
	d, _ := Open(t.TempDir())
	old := selfTicket(t, 5, "older")
	old.Name = old.fileName(time.Now().Add(10 * time.Minute)) // enqueued before the clock went back 10 minutes
	b, _ := old.encode()
	if err := d.writeAtomic(old.Name, b); err != nil {
		t.Fatal(err)
	}
	running := selfTicket(t, 3, "running")
	running.Name = running.fileName(time.Now().Add(20 * time.Minute))
	b, _ = running.encode()
	if err := d.writeAtomic(running.Name+runSuffix, b); err != nil {
		t.Fatal(err)
	}
	for _, lane := range []string{"newer", "newest"} {
		if err := d.Add(selfTicket(t, 5, lane)); err != nil {
			t.Fatal(err)
		}
	}
	s, _ := d.List()
	var got []string
	for _, tk := range s.Waiting {
		got = append(got, tk.Lane)
	}
	if strings.Join(got, ",") != "older,newer,newest" {
		t.Fatalf("queue order after a clock step back = %v, want older,newer,newest", got)
	}
}

// fakePSOnPath puts a ps on PATH that logs each call and runs the real one,
// and returns the log.
func fakePSOnPath(t *testing.T) string {
	t.Helper()
	real, err := exec.LookPath("ps")
	if err != nil {
		t.Skip("no ps here")
	}
	dir := t.TempDir()
	log := filepath.Join(dir, "calls")
	script := fmt.Sprintf("#!/bin/sh\necho \"$*\" >> %q\nexec %q \"$@\"\n", log, real)
	if err := os.WriteFile(filepath.Join(dir, "ps"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return log
}

// With ps as the process source (macOS), pruning N tickets runs ps once, not
// once per ticket: every waiter prunes every poll, so per-ticket ps is N^2.
func TestPruneChecksAllOwnersWithOnePS(t *testing.T) {
	ps := procinfo.ProcFS{UsePS: true}
	if !ps.Available() {
		t.Skip("ps cannot list processes here")
	}
	log := fakePSOnPath(t)
	d, _ := Open(t.TempDir())
	var kids []*exec.Cmd
	for i := range 6 {
		cmd := exec.Command("sleep", "30")
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		kids = append(kids, cmd)
		st, err := ps.StartTime(cmd.Process.Pid)
		if err != nil {
			t.Fatal(err)
		}
		if err := d.Add(&Ticket{Pid: cmd.Process.Pid, Start: st, Prio: 3, Lane: fmt.Sprintf("kid%d", i)}); err != nil {
			t.Fatal(err)
		}
	}
	defer func() {
		for _, k := range kids {
			_ = k.Process.Kill()
			_ = k.Wait()
		}
	}()
	_ = kids[0].Process.Kill()
	_ = kids[0].Wait()
	_ = os.Remove(log)
	d.Prune(ps)
	calls, _ := os.ReadFile(log)
	if n := strings.Count(string(calls), "\n"); n != 1 {
		t.Fatalf("Prune of 6 tickets ran ps %d times, want 1:\n%s", n, calls)
	}
	if s, _ := d.List(); len(s.Waiting) != 5 {
		t.Fatalf("after prune: %d tickets, want the 5 live ones", len(s.Waiting))
	}
}

// A waiter stopped with Ctrl-Z runs no rounds, so it never claims: it must not
// count as a ticket ahead of the others, or it holds them back until resumed.
func TestPruneTellsStoppedOwnersApart(t *testing.T) {
	d, _ := Open(t.TempDir())
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	st, err := procinfo.Default.StartTime(cmd.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Add(&Ticket{Pid: cmd.Process.Pid, Start: st, Prio: 2, Lane: "suspended"}); err != nil {
		t.Fatal(err)
	}
	if err := d.Add(selfTicket(t, 5, "awake")); err != nil {
		t.Fatal(err)
	}
	lanes := func(ts []*Ticket) string {
		var out []string
		for _, tk := range ts {
			out = append(out, tk.Lane)
		}
		return strings.Join(out, ",")
	}
	owners := d.Prune(procinfo.Default)
	s, _ := d.List()
	if got := lanes(owners.Active(s.Waiting)); got != "suspended,awake" {
		t.Fatalf("running owners: active = %s", got)
	}
	if err := cmd.Process.Signal(syscall.SIGSTOP); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		owners = d.Prune(procinfo.Default)
		s, _ = d.List()
		if lanes(owners.Active(s.Waiting)) == "awake" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("stopped owner still active: %s", lanes(owners.Active(s.Waiting)))
		}
		time.Sleep(20 * time.Millisecond)
	}
	if len(s.Waiting) != 2 || !owners.Stopped(s.Waiting[0]) || owners.Stopped(s.Waiting[1]) {
		t.Fatal("a stopped owner keeps its ticket (it may resume) and is reported stopped")
	}
}
