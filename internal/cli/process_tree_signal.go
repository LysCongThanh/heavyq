package cli

import (
	"os"
	"syscall"

	"github.com/LysCongThanh/heavyq/internal/procinfo"
)

// processTree is everything a local command started. On Linux heavyq is a
// child subreaper, so the tree is simply heavyq's own descendants: orphans
// re-parent to us and stay reachable. Elsewhere (macOS has no subreaper) it
// is the command's descendants found by walking the ppid map of `ps`, plus
// every process seen in an earlier scan for as long as it runs (lifelineTree):
// a grandchild that outlives a command that died on SIGTERM re-parents to
// launchd but stays in the tree, so it still gets the SIGKILL pass and heavyq
// keeps the slot until it is gone. A process that was already re-parented
// before the first scan (a daemon the command started and left) can still be
// missed.
type processTree struct {
	root  int           // heavyq itself (subreaper) or the command's pid
	track *lifelineTree // without a subreaper: the pids seen so far
}

func newProcessTree(child int, subreaper bool) *processTree {
	if subreaper {
		return &processTree{root: os.Getpid()}
	}
	return &processTree{root: child, track: &lifelineTree{self: child, seen: map[int]bool{}}}
}

// pids lists the live descendants of the root (and, without a subreaper, the
// processes an earlier scan saw that still run).
func (t *processTree) pids() []int {
	if t == nil {
		return nil
	}
	if t.track != nil {
		return t.track.live()
	}
	return procinfo.Default.Descendants(t.root)
}

// signal sends sig to every descendant of the root (the direct child is also
// signalled by the caller through its exec.Cmd).
func (t *processTree) signal(sig syscall.Signal) {
	for _, pid := range t.pids() {
		_ = syscall.Kill(pid, sig)
	}
}

// alive reports whether any descendant still runs (zombies excluded).
func (t *processTree) alive() bool {
	for _, pid := range t.pids() {
		if procinfo.Default.Running(pid) {
			return true
		}
	}
	return false
}
