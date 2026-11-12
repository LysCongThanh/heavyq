package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/LysCongThanh/heavyq/internal/queue"
)

// A heavyq stopped (Ctrl-Z, SIGSTOP, a debugger) while it holds the queue
// mutex must not hang every other waiter: --max-wait, SIGTERM and cancel still
// work, and the queue moves again as soon as the holder lets go.
func TestStalledMutexHolderCannotHangWaiters(t *testing.T) {
	h := newRig(t, baseConfig)
	d, err := queue.Open(filepath.Join(h.root, "q"))
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(filepath.Join(d.Path, ".mutex"), os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}

	if p := h.start("3", "--lane", "maxwait", "--max-wait", "500ms", "--", "true"); p.wait(t, 20*time.Second) != ExitMaxWait {
		t.Fatalf("--max-wait must fire behind a stalled mutex holder:\n%s", p.out)
	}
	term := h.start("3", "--lane", "term", "--", "true")
	h.waiting(1)
	_ = term.cmd.Process.Signal(syscall.SIGTERM)
	if rc := term.wait(t, 20*time.Second); rc != 143 {
		t.Fatalf("SIGTERM behind a stalled mutex holder: rc=%d\n%s", rc, term.out)
	}
	gone := h.start("3", "--lane", "cancel", "--", "true")
	h.waiting(1)
	if rc, out := h.run("cancel", strconv.Itoa(gone.cmd.Process.Pid)); rc != 0 {
		t.Fatalf("cancel rc=%d: %s", rc, out)
	}
	if rc := gone.wait(t, 20*time.Second); rc != ExitCancelled {
		t.Fatalf("cancel behind a stalled mutex holder: rc=%d\n%s", rc, gone.out)
	}
	if s := h.snapshot(); len(s.Waiting)+len(s.Running) != 0 {
		t.Fatalf("tickets left behind: %+v", s)
	}

	_ = f.Close() // the holder resumes and lets go
	if rc, out := h.run("3", "--max-wait", "20s", "--", "true"); rc != 0 {
		t.Fatalf("the queue must move once the holder lets go: rc=%d\n%s", rc, out)
	}
}

// A waiter suspended with Ctrl-Z runs no rounds and never claims: the tickets
// behind it run, and it gets its turn once resumed.
func TestSuspendedWaiterDoesNotHoldBackTheQueue(t *testing.T) {
	h := newRig(t, baseConfig)
	hold := h.start("5", "--lane", "hold", "--", "sh", "-c", h.gateFile("go"))
	h.running(1)
	// Its own process group, as a shell job has: job-control stops are discarded in an orphaned group.
	susp := h.startWith(&syscall.SysProcAttr{Setpgid: true}, "2", "--lane", "suspended", "--", "true")
	h.waiting(1)
	pid := susp.cmd.Process.Pid
	_ = susp.cmd.Process.Signal(syscall.SIGTSTP)
	h.until("the waiter is stopped", func() bool {
		out, err := exec.Command("ps", "-o", "stat=", "-p", strconv.Itoa(pid)).Output()
		return err == nil && strings.HasPrefix(strings.TrimSpace(string(out)), "T")
	})
	next := h.start("5", "--lane", "next", "--", "true")
	h.waiting(2)
	h.touch("go")
	if rc := hold.wait(t, 20*time.Second); rc != 0 {
		t.Fatalf("hold rc=%d\n%s", rc, hold.out)
	}
	if rc := next.wait(t, 20*time.Second); rc != 0 {
		t.Fatalf("a stopped waiter held back the ticket behind it: rc=%d\n%s", rc, next.out)
	}
	_ = susp.cmd.Process.Signal(syscall.SIGCONT)
	if rc := susp.wait(t, 20*time.Second); rc != 0 {
		t.Fatalf("the resumed waiter must run: rc=%d\n%s", rc, susp.out)
	}
}
