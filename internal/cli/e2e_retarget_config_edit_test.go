package cli

import (
	"slices"
	"strings"
	"testing"
	"time"
)

// A waiting ticket follows edits of the config file (run_config_reload.go) and
// keeps the machines it was given; `heavyq retarget` gives it new ones. The two
// meet here: a move is vetted against the config the ticket follows now, and the
// machines it moved to are the ones it keeps across later edits.

// waitingCandidates are the candidates of the waiting ticket of lane (false when none of that lane waits).
func (h *rig) waitingCandidates(lane string) ([]string, bool) {
	h.t.Helper()
	for _, t := range h.snapshot().Waiting {
		if t.Lane == lane {
			return t.Candidates, true
		}
	}
	return nil, false
}

// slowPoll makes the waiters of cfg poll once a second, so a waiter adopts an
// edit (two polls that read it the same) well after a command that reads the
// file at once.
func slowPoll(cfg string) string { return strings.Replace(cfg, `poll = "50ms"`, `poll = "1s"`, 1) }

// A remote added by an edit can be the target of a move right away: the move
// is filed once the ticket follows the config `heavyq retarget` read, so the
// ticket vets it with the edited config, not with the one it was submitted with.
func TestE2ERetargetToARemoteJustAddedByAnEdit(t *testing.T) {
	h := newRig(t, "")
	h.setConfig(slowPoll(remoteConfig(h, "")))
	gitRepo(t, h)
	hold := h.start("4", "--remote", "box", "--lane", "holdbox", "--", "sh", "-c", h.gateFile("go"))
	h.running(1)
	quick := h.start("3", "--remote", "box", "--lane", "quick", "--", "true")
	h.waiting(1)

	h.setConfig(slowPoll(twoRemotesConfig(h, "")))
	if rc, out := h.run("retarget", pidArg(quick), "--remote", "box2"); rc != 0 || !strings.Contains(out, "now waits for box2") {
		t.Fatalf("move to the added box2: rc=%d\n%s", rc, out)
	}
	if rc := quick.wait(t, 30*time.Second); rc != 0 || h.machineOfLane("quick") != "box2" {
		t.Fatalf("moved ticket: rc=%d, ran on %q\n%s", rc, h.machineOfLane("quick"), quick.out)
	}
	h.touch("go")
	if rc := hold.wait(t, 30*time.Second); rc != 0 {
		t.Fatalf("holder rc=%d\n%s", rc, hold.out)
	}
}

// Moved to auto, a ticket waits for the remotes of the config it moved under
// (one added since it was submitted included) and keeps them when a later edit
// adds another one, as it kept its submitted remotes; it does not snap back to
// the machines it was submitted with either.
func TestE2ERetargetKeepsItsNewMachinesAcrossLaterEdits(t *testing.T) {
	h := newRig(t, "")
	h.setConfig(remoteConfig(h, ""))
	gitRepo(t, h)
	holdLocal := h.start("4", "--remote", "local", "--lane", "holdlocal", "--", "sh", "-c", h.gateFile("go"))
	holdBox := h.start("4", "--remote", "box", "--lane", "holdbox", "--", "sh", "-c", h.gateFile("go"))
	h.running(2)
	waiter := h.start("3", "--remote", "box", "--lane", "waiter", "--", "true")
	h.waiting(1)
	before := h.snapshot().Waiting[0].ConfigSum
	h.setConfig(twoRemotesConfig(h, ""))
	h.until("the waiter adopts the edit", func() bool {
		ws := h.snapshot().Waiting
		return len(ws) == 0 || ws[0].ConfigSum != before
	})

	holdBox2 := h.start("4", "--remote", "box2", "--lane", "holdbox2", "--", "sh", "-c", h.gateFile("go"))
	h.running(3)
	if rc, out := h.run("retarget", pidArg(waiter), "--remote", "auto"); rc != 0 || !strings.Contains(out, "now waits for auto") {
		t.Fatalf("move to auto: rc=%d\n%s", rc, out)
	}
	want := []string{"local", "box", "box2"}
	if got, _ := h.waitingCandidates("waiter"); !slices.Equal(got, want) {
		t.Fatalf("candidates after the move = %v, want the edited config's %v", got, want)
	}

	// A later edit adds a free remote: the ticket keeps what it moved to, it
	// neither adopts "late" nor snaps back to its submitted [box].
	before = h.snapshot().Waiting[0].ConfigSum
	h.setConfig(twoRemotesConfig(h, lateRemote(h)))
	h.until("the waiter adopts the edit", func() bool {
		ws := h.snapshot().Waiting
		return len(ws) == 0 || ws[0].ConfigSum != before
	})
	time.Sleep(400 * time.Millisecond) // several polls: time to claim "late" if it were a candidate
	got, ok := h.waitingCandidates("waiter")
	if !ok {
		t.Fatalf("the moved ticket left the queue (ran on the added remote?):\n%s", waiter.out)
	}
	if !slices.Equal(got, want) || h.waitingTarget("waiter") != "auto" {
		t.Fatalf("after a later edit: target %q, candidates %v; want auto %v", h.waitingTarget("waiter"), got, want)
	}

	h.touch("go")
	for _, p := range []*proc{holdLocal, holdBox, holdBox2, waiter} {
		if rc := p.wait(t, 30*time.Second); rc != 0 {
			t.Fatalf("rc=%d\n%s", rc, p.out)
		}
	}
	if m := h.machineOfLane("waiter"); m == "late" {
		t.Fatalf("the moved ticket ran on the remote added after its move")
	}
}

// The machine a ticket was moved to is removed by an edit: the ticket counts it
// as down, as it would a submitted remote (why names it, exit 76 after
// remote_down_max), and does not fall back to the machines it had before.
func TestE2ERetargetedRemoteRemovedByAnEditIsDown(t *testing.T) {
	h := newRig(t, "")
	gitRepo(t, h)
	down := func(cfg string) string {
		return strings.Replace(cfg, "[queue]\n", "[queue]\nremote_down_max = \"3s\"\n", 1)
	}
	h.setConfig(down(twoRemotesConfig(h, "")))
	holdBox := h.start("4", "--remote", "box", "--lane", "holdbox", "--", "sh", "-c", h.gateFile("go"))
	holdBox2 := h.start("4", "--remote", "box2", "--lane", "holdbox2", "--", "sh", "-c", h.gateFile("go"))
	h.running(2)
	waiter := h.start("3", "--remote", "box", "--lane", "waiter", "--", "true")
	h.waiting(1)
	if rc, out := h.run("retarget", pidArg(waiter), "--remote", "box2"); rc != 0 || !strings.Contains(out, "now waits for box2") {
		t.Fatalf("move to box2: rc=%d\n%s", rc, out)
	}
	before := h.snapshot().Waiting[0].ConfigSum

	h.setConfig(down(remoteConfig(h, ""))) // box2 is gone, box is still there
	h.until("the waiter adopts the edit", func() bool {
		ws := h.snapshot().Waiting
		return len(ws) == 0 || ws[0].ConfigSum != before
	})
	if rc, out := h.run("why", "waiter"); rc != 0 || !strings.Contains(out, "box2: "+notInConfigReason) {
		t.Fatalf("why names the removed remote (rc=%d):\n%s", rc, out)
	}
	if rc := waiter.wait(t, 20*time.Second); rc != ExitRemoteDown || !strings.Contains(waiter.out.String(), "box2 (not in the config)") {
		t.Fatalf("rc=%d, want %d:\n%s", rc, ExitRemoteDown, waiter.out)
	}
	h.touch("go")
	for _, p := range []*proc{holdBox, holdBox2} {
		p.wait(t, 20*time.Second)
	}
}
