//go:build linux

package cli

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"github.com/LysCongThanh/heavyq/internal/procinfo"
)

// zombiesUnder counts the zombie processes among the descendants of root.
func zombiesUnder(root int) int {
	parent := map[int]int{}
	zombie := map[int]bool{}
	entries, _ := os.ReadDir("/proc")
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		b, err := os.ReadFile(filepath.Join("/proc", e.Name(), "stat"))
		if err != nil {
			continue
		}
		f := strings.Fields(string(b[bytes.LastIndexByte(b, ')')+1:]))
		if len(f) < 2 {
			continue
		}
		ppid, _ := strconv.Atoi(f[1])
		parent[pid], zombie[pid] = ppid, f[0] == "Z"
	}
	n := 0
	for pid, z := range zombie {
		for p := parent[pid]; z && p > 1; p = parent[p] {
			if p == root {
				n++
				break
			}
		}
	}
	return n
}

// heavyq adopts the command's orphans (it is their subreaper while the command
// runs): it must reap the ones that exit, not keep a zombie of each until the
// run, or the whole sequential gate, ends.
func TestE2EOrphansOfTheCommandAreReaped(t *testing.T) {
	h := newRig(t, baseConfig)
	cmd := "for i in 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15 16 17 18 19 20; do (sleep 0.01 &); done; sleep 0.5; touch " +
		filepath.Join(h.root, "ready") + "; sleep 3"
	p := h.start("5", "--", "sh", "-c", cmd)
	h.until("the orphans to exit", func() bool { return fileExists(filepath.Join(h.root, "ready")) })
	time.Sleep(300 * time.Millisecond)
	if n := zombiesUnder(p.cmd.Process.Pid); n > 0 {
		t.Fatalf("%d zombies under heavyq while its command runs", n)
	}
	if rc := p.wait(t, 30*time.Second); rc != 0 {
		t.Fatalf("rc=%d\n%s", rc, p.out)
	}
}

// openPTY opens a pseudo-terminal pair (Linux /dev/ptmx).
func openPTY(t *testing.T) (master, slave *os.File) {
	t.Helper()
	m, err := os.OpenFile("/dev/ptmx", os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Skipf("no pseudo-terminals here: %v", err)
	}
	var unlock int32
	if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, m.Fd(), syscall.TIOCSPTLCK, uintptr(unsafe.Pointer(&unlock))); e != 0 {
		t.Skipf("unlockpt: %v", e)
	}
	var n uint32
	if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, m.Fd(), syscall.TIOCGPTN, uintptr(unsafe.Pointer(&n))); e != 0 {
		t.Skipf("ptsname: %v", e)
	}
	s, err := os.OpenFile(fmt.Sprintf("/dev/pts/%d", n), os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Skipf("open the pty: %v", err)
	}
	t.Cleanup(func() { m.Close(); s.Close() })
	return m, s
}

// `gate run --parallel` from a terminal: a command that wants to prompt on
// /dev/tty must fail at once, not stop in the background (SIGTTIN) and hang
// the gate with its slot held.
func TestE2EGateParallelCommandCannotStopOnTheTerminal(t *testing.T) {
	h := newRig(t, baseConfig)
	gateRepoRuns(t, h, "read answer < /dev/tty")
	master, slave := openPTY(t)
	go func() { _, _ = io.Copy(io.Discard, master) }()
	cmd := exec.Command(binPath, "gate", "run", "--parallel")
	cmd.Dir, cmd.Env = h.cwd, h.env
	cmd.Stdin, cmd.Stdout, cmd.Stderr = slave, slave, slave
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, pid := range procinfo.Default.Descendants(cmd.Process.Pid) {
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
		_ = cmd.Process.Kill()
	})
	done := make(chan int, 1)
	go func() { done <- processExit(cmd.Wait()) }()
	select {
	case rc := <-done:
		if rc != 1 {
			t.Fatalf("rc=%d: the prompt has no terminal, so the command fails and the gate is not proven", rc)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("gate run --parallel hangs: its command was stopped trying to read the terminal")
	}
}
