package cli

import (
	"strconv"
	"strings"
	"testing"
	"time"
)

// `heavyq retarget <ticket|pid> --remote WHERE` changes the machines a waiting
// ticket may run on, on its next poll, without cancelling it: it keeps its
// priority and its place. These tests use the fake remotes "box" and "box2"
// (reached through `sh -c`, see twoRemotesConfig): a ticket waits for a busy
// box, is moved, and runs on its new machine.

func pidArg(p *proc) string { return strconv.Itoa(p.cmd.Process.Pid) }

// waitingTarget is the target the waiting ticket of lane records now ("" when none of that lane waits).
func (h *rig) waitingTarget(lane string) string {
	h.t.Helper()
	for _, t := range h.snapshot().Waiting {
		if t.Lane == lane {
			return t.Target
		}
	}
	return ""
}

func TestE2ERetargetMovesAWaitingTicketToItsNewMachine(t *testing.T) {
	h := newRig(t, "")
	h.setConfig(twoRemotesConfig(h, ""))
	gitRepo(t, h)
	hold := h.start("3", "--remote", "box", "--lane", "hold", "--", "sh", "-c", h.gateFile("go"))
	h.running(1)

	// Waiting for the busy box, moved to box2 by pid: it ships the commit it was queued with and runs there.
	away := h.start("3", "--remote", "box", "--lane", "away", "--", "cat", "marker.txt")
	h.waiting(1)
	if rc, out := h.run("retarget", pidArg(away), "--remote", "box2"); rc != 0 || !strings.Contains(out, "now waits for box2") {
		t.Fatalf("retarget to box2: rc=%d\n%s", rc, out)
	}
	if rc := away.wait(t, 30*time.Second); rc != 0 || !strings.Contains(away.out.String(), "from-commit") ||
		!strings.Contains(away.out.String(), "moved to box2 by an operator") {
		t.Fatalf("moved ticket: rc=%d\n%s", rc, away.out)
	}
	if got := h.machineOfLane("away"); got != "box2" {
		t.Fatalf("the moved ticket ran on %q, want box2", got)
	}

	// Waiting for the busy box again, moved to this machine by its ticket id: it runs here, in place.
	home := h.start("3", "--remote", "box", "--lane", "home", "--", "sh", "-c", "echo on-$HEAVYQ_MACHINE")
	h.waiting(1)
	if rc, out := h.run("retarget", h.snapshot().Waiting[0].Name, "--remote", "local"); rc != 0 ||
		!strings.Contains(out, "now waits for local") {
		t.Fatalf("retarget to local: rc=%d\n%s", rc, out)
	}
	if rc := home.wait(t, 30*time.Second); rc != 0 || !strings.Contains(home.out.String(), "on-local") {
		t.Fatalf("moved ticket: rc=%d\n%s", rc, home.out)
	}
	if got := h.machineOfLane("home"); got != "local" {
		t.Fatalf("the moved ticket ran on %q, want local", got)
	}
	h.touch("go")
	if rc := hold.wait(t, 30*time.Second); rc != 0 {
		t.Fatalf("holder rc=%d\n%s", rc, hold.out)
	}
}

// A ticket pinned to an older commit (--ref) cannot run on this machine, which
// runs the work tree in place. Only its own process knows that: it refuses the
// move, `heavyq retarget` says why with exit 2, and the ticket keeps waiting for
// its machine, where it later runs the commit it was queued with.
func TestE2ERetargetKeepsTheMachinesOfATicketThisMachineCannotRun(t *testing.T) {
	h := newRig(t, "")
	h.setConfig(remoteConfig(h, ""))
	repo := gitRepo(t, h)
	old := gitOutput(t, h, repo, "rev-parse", "HEAD")
	commitIn(t, h, repo, "marker.txt", "newer\n")
	hold := h.start("3", "--remote", "box", "--lane", "hold", "--", "sh", "-c", h.gateFile("go"))
	h.running(1)
	pinned := h.start("3", "--remote", "box", "--ref", old, "--lane", "pinned", "--", "cat", "marker.txt")
	h.waiting(1)
	rc, out := h.run("retarget", pidArg(pinned), "--remote", "local")
	if rc != ExitUsage || !strings.Contains(out, "kept its machines") || !strings.Contains(out, "not the checked-out HEAD") {
		t.Fatalf("a move this machine cannot run: rc=%d, want %d with the reason\n%s", rc, ExitUsage, out)
	}
	if got := h.waitingTarget("pinned"); got != "box" {
		t.Fatalf("target after a refused move = %q, want box", got)
	}
	h.touch("go")
	if rc := hold.wait(t, 30*time.Second); rc != 0 {
		t.Fatalf("holder rc=%d\n%s", rc, hold.out)
	}
	if rc := pinned.wait(t, 30*time.Second); rc != 0 || !strings.Contains(pinned.out.String(), "from-commit") {
		t.Fatalf("pinned ticket: rc=%d\n%s", rc, pinned.out)
	}
	if got := h.machineOfLane("pinned"); got != "box" {
		t.Fatalf("the pinned ticket ran on %q, want box", got)
	}
}
