package cli

import (
	"os"
	"syscall"

	"github.com/LysCongThanh/heavyq/internal/procinfo"
)

// processTree is everything a local command started. On Linux heavyq is a
// child subreaper, so the tree is simply heavyq's own descendants: orphans
// re-parent to us and stay reachable. Elsewhere (macOS has no subreaper) it
// is the command's descendants as seen at signal time, found by walking the
// ppid map of `ps`: a cancel reaches every grandchild still attached to its
// parent, but a process whose parent already exited has re-parented to
// launchd and can be missed (typically a daemon started by the command, or
// a grandchild that outlived a command that died on SIGTERM before the
// SIGKILL pass).
type processTree struct {
	root int // heavyq itself (subreaper) or the command's pid
}

func newProcessTree(child int, subreaper bool) *processTree {
	if subreaper {
		return &processTree{root: os.Getpid()}
	}
	return &processTree{root: child}
}

// pids lists the live descendants of the root.
func (t *processTree) pids() []int {
	if t == nil {
		return nil
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
