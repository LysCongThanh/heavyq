package cli

import (
	"slices"
	"strings"
	"testing"
	"time"
)

// Placement by tools (#148) is decided before a ticket is queued. Two things
// change a waiting ticket's machines later: `heavyq retarget` and an edit of
// the config file. These tests use the remotes of e2e_remote_tool_placement_test.go
// and e2e_remote_tool_cache_test.go: a tool resolves where a remote's env_path holds it.

// A move is placed by tools as a run is: a move whose machines all lack a tool
// the run needs (its argv[0] or --tools) is refused by the ticket, naming the
// tools and env_path, and it keeps its machines; in a list, a machine that
// lacks one is skipped with the reason in why.
func TestE2ERetargetPlacesTheMoveByTools(t *testing.T) {
	h := newRig(t, "")
	toolRemotes(t, h, "") // box lacks onlyTool, box2 has it
	holdLocal := h.start("4", "--remote", "local", "--lane", "holdlocal", "--", "sh", "-c", h.gateFile("go"))
	holdBox2 := h.start("4", "--remote", "box2", "--lane", "holdbox2", "--", "sh", "-c", h.gateFile("go"))
	h.running(2)
	argv0 := h.start("3", "--remote", "box2", "--lane", "argv0", "--", onlyTool)
	named := h.start("3", "--remote", "box2", "--tools", onlyTool, "--lane", "named", "--", "true")
	h.waiting(2)

	rc, out := h.run("retarget", pidArg(argv0), "--remote", "box")
	if rc != ExitUsage || !strings.Contains(out, "kept its machines") || !strings.Contains(out, "box lacks "+onlyTool) ||
		!strings.Contains(out, "env_path") {
		t.Fatalf("move onto a remote without the command: rc=%d, want %d naming the tool and env_path\n%s", rc, ExitUsage, out)
	}
	if got := h.waitingTarget("argv0"); got != "box2" {
		t.Fatalf("target after a refused move = %q, want box2", got)
	}

	if rc, out := h.run("retarget", pidArg(named), "--remote", "box,local"); rc != 0 || !strings.Contains(out, "now waits for box,local") {
		t.Fatalf("move to a list: rc=%d\n%s", rc, out)
	}
	if got, _ := h.waitingCandidates("named"); !slices.Equal(got, []string{"local"}) {
		t.Fatalf("candidates after the move = %v, want [local] (box lacks %s)", got, onlyTool)
	}
	if rc, out := h.run("why", "named"); rc != 0 || !strings.Contains(out, "box: lacks "+onlyTool) {
		t.Fatalf("why names the skipped remote (rc=%d):\n%s", rc, out)
	}

	h.touch("go")
	for _, p := range []*proc{holdLocal, holdBox2, argv0, named} {
		if rc := p.wait(t, 30*time.Second); rc != 0 {
			t.Fatalf("rc=%d\n%s", rc, p.out)
		}
	}
	if got := h.machineOfLane("argv0"); got != "box2" {
		t.Fatalf("the refused ticket ran on %q, want box2", got)
	}
	if got := h.machineOfLane("named"); got != "local" {
		t.Fatalf("the moved ticket ran on %q, want local", got)
	}
}

// envPathsConfig is box and box2 with the env_path of each.
func envPathsConfig(h *rig, box, box2 string) string {
	cfg := twoRemotesConfig(h, "env_path = \""+box2+"\"\n")
	return strings.Replace(cfg, "name = \"box\"\n", "name = \"box\"\nenv_path = \""+box+"\"\n", 1)
}

// What tools placement found is about the PATH a job got then. An edit of a
// candidate's env_path makes the waiting ticket ask again: the remote that
// now has the tool becomes a candidate, the one that lost it is skipped, and a
// ticket no machine is left for gives up with exit 2, as it would be refused at
// submit.
func TestE2EWaitingTicketAsksForToolsAgainWhenAnEditChangesEnvPath(t *testing.T) {
	h := newRig(t, "")
	withTool, empty := toolDirWith(t, cachedTool), toolDirWith(t)
	h.setConfig(envPathsConfig(h, empty, withTool))
	gitRepo(t, h)
	hold := h.start("4", "--remote", "box2", "--lane", "holdbox2", "--", "sh", "-c", h.gateFile("go"))
	h.running(1)
	waiter := h.start("3", "--remote", "box,box2", "--tools", cachedTool, "--lane", "waiter", "--", "true")
	h.waiting(1)
	if got, _ := h.waitingCandidates("waiter"); !slices.Equal(got, []string{"box2"}) {
		t.Fatalf("candidates at submit = %v, want [box2] (box lacks %s)", got, cachedTool)
	}

	h.setConfig(envPathsConfig(h, withTool, empty)) // the tool is now on box's PATH, and no longer on box2's
	if rc := waiter.wait(t, 20*time.Second); rc != 0 || h.machineOfLane("waiter") != "box" {
		t.Fatalf("rc=%d on %q, want 0 on box (box2, still busy, lost the tool):\n%s", rc, h.machineOfLane("waiter"), waiter.out)
	}

	only := h.start("3", "--remote", "box", "--tools", cachedTool, "--lane", "only", "--", "sh", "-c", h.gateFile("never"))
	h.until("only runs on box", func() bool { return len(h.snapshot().Running) == 2 })
	late := h.start("3", "--remote", "box", "--tools", cachedTool, "--lane", "late", "--", "true")
	h.waiting(1)
	h.setConfig(envPathsConfig(h, empty, empty))
	if rc := late.wait(t, 20*time.Second); rc != ExitUsage || !strings.Contains(late.out.String(), "box lacks "+cachedTool) {
		t.Fatalf("rc=%d, want %d naming what box lacks:\n%s", rc, ExitUsage, late.out)
	}
	h.touch("go")
	h.touch("never")
	for _, p := range []*proc{hold, only} {
		p.wait(t, 20*time.Second)
	}
}
