package cli

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/LysCongThanh/heavyq/internal/history"
	"github.com/LysCongThanh/heavyq/internal/procinfo"
)

// silentShipRig is a rig whose remote "box" takes the commit over ssh ("host:path" git URL), with git's ssh
// replaced by a stub that never answers, the way a link that went silent behaves. It returns the file in
// which the stub writes its pid once git started it.
func silentShipRig(t *testing.T) (*rig, string) {
	t.Helper()
	h := newRig(t, "")
	work := filepath.Join(h.root, "remote-work")
	h.setConfig(baseConfig + `[[remote]]
name = "box"
transport = ["sh", "-c"]
work_dir = "` + work + `"
git_url = "fakehost:` + filepath.Join(work, "git") + `"
heavy_slots = 1
`)
	gitRepo(t, h)
	pidFile := filepath.Join(h.root, "ssh.pid")
	stub := filepath.Join(h.root, "silent-ssh")
	if err := os.WriteFile(stub, []byte("#!/bin/sh\necho $$ > '"+pidFile+"'\nexec sleep 60\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	h.env = append(h.env, "GIT_SSH_COMMAND="+stub)
	return h, pidFile
}

// stubPid waits for the silent ssh to start and returns its pid.
func stubPid(t *testing.T, h *rig, pidFile string) int {
	t.Helper()
	var pid int
	h.until("git push to reach its ssh", func() bool {
		b, err := os.ReadFile(pidFile)
		if err != nil {
			return false
		}
		pid, err = strconv.Atoi(strings.TrimSpace(string(b)))
		return err == nil
	})
	t.Cleanup(func() { _ = syscall.Kill(pid, syscall.SIGKILL) })
	return pid
}

// assertGone fails unless pid stops running within 5 s. A zombie counts as gone: when these tests run
// under a heavyq (a subreaper on Linux), the killed ssh is reparented to it and lingers as a zombie.
func assertGone(t *testing.T, pid int) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if !procinfo.Default.Running(pid) {
			return
		}
	}
	t.Fatalf("the push's ssh (pid %d) outlived the run", pid)
}

// SIGTERM to heavyq while it ships the commit used to wait in a channel until the push ended (never, on a
// silent link), with the slot held. It now stops the push and exits 143 as an interrupted run.
func TestE2ESigtermWhileShippingStopsTheRun(t *testing.T) {
	h, pidFile := silentShipRig(t)
	p := h.start("3", "--remote", "box", "--", "true")
	pid := stubPid(t, h, pidFile)
	_ = p.cmd.Process.Signal(syscall.SIGTERM)
	if rc := p.wait(t, 10*time.Second); rc != 143 {
		t.Fatalf("rc=%d, want 143\n%s", rc, p.out)
	}
	assertGone(t, pid)
	if r := h.lastRecord(); r.Result != history.OutcomeInterrupted {
		t.Fatalf("recorded as %+v, want interrupted", r)
	}
}

// `heavyq cancel` while the commit ships used to do nothing: cancel requests were only read once the command
// ran. It now stops the push and the run exits 78.
func TestE2ECancelWhileShippingStopsTheRun(t *testing.T) {
	h, pidFile := silentShipRig(t)
	p := h.start("3", "--remote", "box", "--", "true")
	pid := stubPid(t, h, pidFile)
	if rc, out := h.run("cancel", strconv.Itoa(p.cmd.Process.Pid)); rc != 0 {
		t.Fatalf("cancel: rc=%d\n%s", rc, out)
	}
	if rc := p.wait(t, 15*time.Second); rc != ExitCancelled {
		t.Fatalf("rc=%d, want %d\n%s", rc, ExitCancelled, p.out)
	}
	assertGone(t, pid)
	if r := h.lastRecord(); r.Result != history.OutcomeCancelled {
		t.Fatalf("recorded as %+v, want cancelled", r)
	}
}
