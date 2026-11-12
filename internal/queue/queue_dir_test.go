package queue

import (
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/LysCongThanh/heavyq/internal/procinfo"
)

func selfTicket(t *testing.T, prio int, lane string) *Ticket {
	t.Helper()
	st, err := procinfo.Default.StartTime(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	return &Ticket{Pid: os.Getpid(), Start: st, Prio: prio, Lane: lane, Candidates: []string{"local"}, Since: time.Now()}
}

func TestQueueOrderPriorityThenFIFO(t *testing.T) {
	d, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, x := range []struct {
		p    int
		lane string
	}{{5, "first5"}, {2, "first2"}, {5, "second5"}, {9, "nine"}, {2, "second2"}} {
		if err := d.Add(selfTicket(t, x.p, x.lane)); err != nil {
			t.Fatal(err)
		}
	}
	s, err := d.List()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"first2", "second2", "first5", "second5", "nine"}
	if len(s.Waiting) != len(want) {
		t.Fatalf("listed %d tickets, want %d: %+v", len(s.Waiting), len(want), s.Waiting)
	}
	for i, tk := range s.Waiting {
		if tk.Lane != want[i] {
			t.Fatalf("position %d: got %s want %s", i, tk.Lane, want[i])
		}
	}
}

func TestClaimMovesToRunning(t *testing.T) {
	d, _ := Open(t.TempDir())
	tk := selfTicket(t, 3, "api")
	if err := d.Add(tk); err != nil {
		t.Fatal(err)
	}
	if err := d.Claim(tk, "local", "heavy-1"); err != nil {
		t.Fatal(err)
	}
	s, _ := d.List()
	if len(s.Waiting) != 0 || len(s.Running) != 1 || s.Running[0].Slot != "heavy-1" || s.Running[0].ClaimedAt.IsZero() {
		t.Fatalf("snapshot after claim: %+v", s)
	}
	d.Remove(tk)
	if d.Exists(tk) {
		t.Fatal("ticket still exists after Remove")
	}
}

func TestPruneRemovesDeadAndRecycledPids(t *testing.T) {
	d, _ := Open(t.TempDir())
	live := selfTicket(t, 3, "live")
	cmd := exec.Command("true")
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	dead := &Ticket{Pid: cmd.Process.Pid, Start: "1", Prio: 3, Lane: "dead"}
	recycled := selfTicket(t, 3, "recycled")
	recycled.Start = "not-our-start-time" // same pid, different process
	for _, tk := range []*Ticket{live, dead, recycled} {
		if err := d.Add(tk); err != nil {
			t.Fatal(err)
		}
	}
	d.Prune(procinfo.Default)
	s, _ := d.List()
	if len(s.Waiting) != 1 || s.Waiting[0].Lane != "live" {
		t.Fatalf("after prune: %+v", s.Waiting)
	}
}

func TestSanitizeLane(t *testing.T) {
	cases := map[string]string{"api": "api", "my lane/x": "my_lane_x", "": "lane", "..": "lane", "a*b": "a_b", "_x": "x", "-": "lane"}
	for in, want := range cases {
		if got := SanitizeLane(in); got != want {
			t.Errorf("SanitizeLane(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestMutexIsExclusive(t *testing.T) {
	d, _ := Open(t.TempDir())
	unlock, err := d.Mutex(time.Second, nil)
	if err != nil {
		t.Fatal(err)
	}
	got := make(chan struct{})
	go func() {
		u, err := d.Mutex(10*time.Second, nil)
		if err == nil {
			u()
		}
		close(got)
	}()
	select {
	case <-got:
		t.Fatal("second Mutex acquired while first is held")
	case <-time.After(150 * time.Millisecond):
	}
	unlock()
	select {
	case <-got:
	case <-time.After(2 * time.Second):
		t.Fatal("second Mutex never acquired")
	}
}

func TestOpenRejectsSymlinkAndTightensPerms(t *testing.T) {
	base := t.TempDir()
	real := base + "/real"
	if err := os.Mkdir(real, 0o777); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(real, 0o777); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(real); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(real); fi.Mode().Perm() != 0o700 {
		t.Fatalf("perm = %v, want 0700", fi.Mode().Perm())
	}
	if err := os.Symlink(real, base+"/link"); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(base + "/link"); err == nil {
		t.Fatal("a symlinked queue dir must be refused")
	}
}
