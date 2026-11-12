package cli

import (
	"os"
	"syscall"
	"time"

	"github.com/LysCongThanh/heavyq/internal/procinfo"
)

// lifelineKillFor bounds the SIGKILL passes once the grace is over: a process
// SIGKILL cannot end (stuck in the kernel) must not keep the lifeline, and so
// the pipe and tree it reports on, alive forever.
const lifelineKillFor = 10 * time.Second

// lifelineTree is what a lifeline stops once heavyq is gone (and, on macOS, what
// heavyq itself stops on a cancel, see processTree): everything below
// it, and everything that was below it. On macOS (no subreaper) a process whose
// parent exits re-parents to launchd and drops out of a scan of our own
// descendants, so each pid seen once stays in the tree, with what is below it,
// for as long as it runs. A tracked pid is dropped the moment it is seen gone,
// so the window for a recycled pid is one scan (tens of milliseconds, against
// the sequential pid allocation of macOS).
type lifelineTree struct {
	self int
	seen map[int]bool
}

// live scans the tree and returns its running processes (zombies excluded:
// the adopted orphans nobody reaps once the command itself has been reaped).
func (t *lifelineTree) live() []int {
	roots := []int{t.self}
	for pid := range t.seen {
		roots = append(roots, pid)
	}
	cand := procinfo.Default.Descendants(roots...)
	for pid := range t.seen {
		cand = append(cand, pid)
	}
	next := map[int]bool{}
	var out []int
	for _, pid := range cand {
		if pid != t.self && !next[pid] && procinfo.Default.Running(pid) {
			next[pid] = true
			out = append(out, pid)
		}
	}
	t.seen = next
	return out
}

func signalPids(pids []int, sig syscall.Signal) {
	for _, pid := range pids {
		_ = syscall.Kill(pid, sig)
	}
}

// stopOwnTree ends everything below this process. It first freezes the tree
// (SIGSTOP, rescanning until nothing new appears): a stopped process can
// neither fork nor exit, so every process is found while still attached to its
// parent. Then SIGTERM and SIGCONT, SIGKILL after lifelineGrace, again until
// nothing is left or lifelineKillFor has passed.
func stopOwnTree() {
	t := &lifelineTree{self: os.Getpid(), seen: map[int]bool{}}
	frozen := map[int]bool{}
	for i := 0; i < 50; i++ {
		before := len(t.seen)
		for _, pid := range t.live() {
			_ = syscall.Kill(pid, syscall.SIGSTOP)
			frozen[pid] = true
		}
		if i > 0 && len(t.seen) == before {
			break
		}
	}
	// Every process stopped above gets SIGTERM and SIGCONT, whatever the next
	// scan says: one left stopped would never end before the SIGKILL pass.
	pids := t.live()
	for pid := range frozen {
		if !t.seen[pid] {
			pids = append(pids, pid)
		}
	}
	signalPids(pids, syscall.SIGTERM)
	signalPids(pids, syscall.SIGCONT)
	for deadline := time.Now().Add(lifelineGrace); len(t.live()) > 0 && time.Now().Before(deadline); {
		time.Sleep(50 * time.Millisecond)
	}
	for deadline := time.Now().Add(lifelineKillFor); time.Now().Before(deadline); {
		pids := t.live()
		if len(pids) == 0 {
			return
		}
		signalPids(pids, syscall.SIGKILL)
		time.Sleep(50 * time.Millisecond)
	}
}
