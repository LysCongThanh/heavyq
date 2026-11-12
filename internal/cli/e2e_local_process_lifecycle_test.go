package cli

import (
	"bufio"
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/LysCongThanh/heavyq/internal/procinfo"
)

// pidFrom waits for name in the rig root to hold a pid and returns it.
func (h *rig) pidFrom(name string) int {
	h.t.Helper()
	h.until(name, func() bool { return atoiTrim(h.read(name)) > 0 })
	pid := atoiTrim(h.read(name))
	h.t.Cleanup(func() { _ = syscall.Kill(pid, syscall.SIGKILL) })
	return pid
}

// gone waits until pid no longer runs (a zombie does not count: under a
// subreaper the test may run in, it waits there to be reaped).
func (h *rig) gone(what string, pid int) {
	h.t.Helper()
	h.until(what+" to stop", func() bool { return !procinfo.Default.Running(pid) })
}

// SIGTERM to heavyq stops everything the command started, not only the
// shell: a compound command's running stage must not outlive the slot. The
// SIGTERM reaches the grandchild itself, so it ends long before the SIGKILL
// that cancelGrace would bring.
func TestE2ESigtermStopsTheWholeCommandTree(t *testing.T) {
	h := newRig(t, baseConfig)
	p := h.start("5", "--", "sh", "-c", "sleep 45 & echo $! > "+filepath.Join(h.root, "child.pid")+"; wait; echo after")
	pid := h.pidFrom("child.pid")
	t0 := time.Now()
	_ = p.cmd.Process.Signal(syscall.SIGTERM)
	if rc := p.wait(t, 30*time.Second); rc != 143 {
		t.Fatalf("rc=%d\n%s", rc, p.out)
	}
	if took := time.Since(t0); took >= cancelGrace-time.Second {
		t.Fatalf("heavyq took %s to stop: the grandchild got no SIGTERM, only the SIGKILL after the grace", took)
	}
	h.gone("the grandchild", pid)
}

// heavyq keeps its slot until the whole stopped tree is gone: a grandchild
// that ignores SIGTERM gets SIGKILL after the grace, and heavyq exits only
// then, never while it still runs. The grandchild does not hold the command's
// output, so nothing but the tree itself keeps heavyq waiting.
func TestE2ESigtermKeepsTheSlotUntilAGrandchildThatIgnoresItIsGone(t *testing.T) {
	h := newRig(t, baseConfig)
	inner := `sh -c 'trap "" TERM; sleep 45' >/dev/null 2>&1 & echo $! > ` + filepath.Join(h.root, "child.pid") + "; wait"
	p := h.start("5", "--", "sh", "-c", inner)
	pid := h.pidFrom("child.pid")
	_ = p.cmd.Process.Signal(syscall.SIGTERM)
	if rc := p.wait(t, 30*time.Second); rc != 143 {
		t.Fatalf("rc=%d\n%s", rc, p.out)
	}
	if procinfo.Default.Running(pid) {
		t.Fatal("heavyq exited (and freed its slot) while a grandchild that ignores SIGTERM still runs")
	}
}

// A Ctrl-C of `gate run --parallel` reaches its children through the gate
// (they have no terminal); every command tree must stop with them.
func TestE2EGateParallelInterruptStopsEveryCommandTree(t *testing.T) {
	h := newRig(t, baseConfig)
	p1, p2 := filepath.Join(h.root, "p1"), filepath.Join(h.root, "p2")
	gateRepoRuns(t, h, "sleep 41 & echo $! > "+p1+"; wait", "sleep 42 & echo $! > "+p2+"; wait")
	h.setConfig(strings.Replace(baseConfig, "heavy_slots = 1", "heavy_slots = 2", 1))
	g := h.start("gate", "run", "--parallel")
	a, b := h.pidFrom("p1"), h.pidFrom("p2")
	_ = g.cmd.Process.Signal(syscall.SIGINT)
	if rc := g.wait(t, 30*time.Second); rc != 130 {
		t.Fatalf("rc=%d\n%s", rc, g.out)
	}
	h.gone("the first command's sleep", a)
	h.gone("the second command's sleep", b)
}

// heavyq killed by SIGKILL cannot clean up, but its command must not run on
// without it: the slot and pool values it held are free at once, so the next
// job would share them with a command nobody accounts for.
func TestE2EKilledHeavyqTakesItsCommandAlong(t *testing.T) {
	h := newRig(t, baseConfig)
	p := h.start("5", "--", "sh", "-c", "sleep 60 & echo $! > "+filepath.Join(h.root, "child.pid")+"; echo $$ > "+
		filepath.Join(h.root, "shell.pid")+"; wait")
	pid, shell := h.pidFrom("child.pid"), h.pidFrom("shell.pid")
	_ = p.cmd.Process.Signal(syscall.SIGKILL)
	p.wait(t, 10*time.Second)
	h.gone("the shell", shell)
	h.gone("the grandchild", pid)
}

// The SIGKILL pass must happen too: a grandchild that ignores SIGTERM, below a
// shell that does not, outlives the SIGTERM pass and the end of the command
// (on macOS re-parented to launchd), and must still be stopped.
func TestE2EKilledHeavyqStopsAGrandchildThatIgnoresSigterm(t *testing.T) {
	h := newRig(t, baseConfig)
	inner := filepath.Join(h.root, "inner.sh")
	script := "trap '' TERM\necho $$ > " + filepath.Join(h.root, "inner.pid") + "\nsleep 40\n"
	if err := os.WriteFile(inner, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	p := h.start("5", "--", "sh", "-c", "sh "+inner+" & wait")
	pid := h.pidFrom("inner.pid")
	_ = p.cmd.Process.Signal(syscall.SIGKILL)
	p.wait(t, 10*time.Second)
	h.gone("the grandchild that ignores SIGTERM", pid)
}

// `heavyq ... -- cmd | head -1`: once head has what it wants, heavyq's stdout
// is a broken pipe. heavyq must not die of SIGPIPE (which frees the slot while
// the command runs on, and writes no ledger line): it keeps supervising the
// command to its end and records it.
func TestE2EBrokenOutputPipeDoesNotKillHeavyq(t *testing.T) {
	h := newRig(t, baseConfig)
	marker := filepath.Join(h.root, "finished")
	cmd := exec.Command(binPath, "5", "--", "sh", "-c", "echo one; sleep 0.5; echo two; sleep 1.5; touch "+marker)
	cmd.Dir, cmd.Env = h.cwd, h.env
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	var errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = pw, &errb
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill() })
	pw.Close()
	line, _ := bufio.NewReader(pr).ReadString('\n')
	pr.Close() // the reader is gone, like head -1
	rc := processExit(cmd.Wait())
	if _, err := os.Stat(marker); err != nil || rc != 0 || line != "one\n" {
		t.Fatalf("heavyq ended before its command (rc=%d, first line %q, finished: %v):\n%s", rc, line, err == nil, errb.String())
	}
	if r := h.lastRecord(); r.Exit != 0 || r.Result != "" {
		t.Fatalf("the run must be in the ledger: %+v", r)
	}
}
