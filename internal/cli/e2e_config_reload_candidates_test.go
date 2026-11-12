package cli

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// What a waiting ticket keeps across a config edit: its candidate machines.
// An auto ticket with --with siblings (or a --ref that is not HEAD) re-checks on
// every poll whether local is still the code it asked for, so its candidates
// are recomputed while it waits; that must not make it adopt a remote the
// edit added, which a plain auto ticket does not adopt either.

// lateRemote is a second fake remote, "late", for the config edit.
func lateRemote(h *rig) string {
	work := filepath.Join(h.root, "late-work")
	return `[[remote]]
name = "late"
transport = ["sh", "-c"]
work_dir = "` + work + `"
git_url = "` + filepath.Join(work, "git") + `"
heavy_slots = 1
`
}

func TestE2EWaitingAutoTicketsKeepTheirCandidatesWhenARemoteIsAdded(t *testing.T) {
	h, _, _ := withRig(t, "")
	h.setConfig(remoteConfig(h, ""))
	holdLocal := h.start("4", "--lane", "holdlocal", "--remote", "local", "--", "sh", "-c", h.gateFile("go"))
	holdBox := h.start("4", "--lane", "holdbox", "--remote", "box", "--", "sh", "-c", h.gateFile("go"))
	h.running(2)
	with := h.start("3", "--lane", "with", "--with", "lib", "--", "true")
	plain := h.start("3", "--lane", "plain", "--", "true")
	h.waiting(2)
	before := h.snapshot().Waiting[0].ConfigSum

	h.setConfig(remoteConfig(h, "") + lateRemote(h))
	h.until("the waiters adopt the edit", func() bool {
		for _, w := range h.snapshot().Waiting {
			if w.ConfigSum == before {
				return false
			}
		}
		return true
	})
	time.Sleep(400 * time.Millisecond) // several polls: time to probe and claim "late" if it were a candidate
	snap := h.snapshot()
	if len(snap.Waiting) != 2 {
		t.Fatalf("a waiter left the queue (ran on the added remote?):\nwith:\n%s\nplain:\n%s", with.out, plain.out)
	}
	for _, w := range snap.Waiting {
		if !slices.Equal(w.Candidates, []string{"local", "box"}) {
			t.Fatalf("%s candidates after the edit = %v, want the submitted [local box]", w.Lane, w.Candidates)
		}
	}

	h.touch("go")
	for _, p := range []*proc{holdLocal, holdBox, with, plain} {
		if rc := p.wait(t, 30*time.Second); rc != 0 {
			t.Fatalf("rc=%d:\n%s", rc, p.out)
		}
	}
	for _, r := range h.history() {
		if r.Machine == "late" {
			t.Fatalf("%s ran on the added remote", r.Lane)
		}
	}
}

// A candidate remote removed from the file is no longer used: a ticket that can
// only run there counts it as down from its next poll (even though it answered
// a probe before the edit), says why, and exits 76 after remote_down_max.
func TestE2EWaitingTicketCountsARemovedRemoteAsDown(t *testing.T) {
	h := newRig(t, "")
	gitRepo(t, h)
	down := func(cfg string) string {
		return strings.Replace(cfg, "[queue]\n", "[queue]\nremote_down_max = \"3s\"\n", 1)
	}
	h.setConfig(down(remoteConfig(h, "")))
	hold := h.start("4", "--lane", "holdbox", "--remote", "box", "--", "sh", "-c", h.gateFile("go"))
	h.running(1)
	waiter := h.start("3", "--lane", "waiter", "--remote", "box", "--", "true")
	h.waiting(1)
	before := h.snapshot().Waiting[0].ConfigSum

	h.setConfig(down(baseConfig))
	h.until("the waiter adopts the edit", func() bool {
		ws := h.snapshot().Waiting
		return len(ws) == 0 || ws[0].ConfigSum != before
	})
	if rc, out := h.run("why", "waiter"); rc != 0 || !strings.Contains(out, "box: "+notInConfigReason) {
		t.Fatalf("why names the removed remote (rc=%d):\n%s", rc, out)
	}
	if rc := waiter.wait(t, 20*time.Second); rc != ExitRemoteDown || !strings.Contains(waiter.out.String(), "box (not in the config)") {
		t.Fatalf("rc=%d, want %d:\n%s", rc, ExitRemoteDown, waiter.out)
	}
	h.touch("go")
	hold.wait(t, 20*time.Second)
}
