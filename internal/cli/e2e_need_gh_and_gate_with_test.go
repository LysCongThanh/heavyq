package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// `--need-gh` (and [run] need_gh_repos) keep a run on this machine, and a
// [[gate]] can carry `with` and `need_gh` through `heavyq gate run`.

// occupyLocal takes the only local slot until the file "go" appears, so a run
// that could go to the remote "box" would do so, and one pinned here must wait.
func occupyLocal(h *rig) *proc {
	h.t.Helper()
	p := h.start("3", "--remote", "local", "--", "sh", "-c", h.gateFile("go"))
	h.running(1)
	return p
}

// stayedLocal starts heavyq with args, checks the ticket waits for a local slot
// (it was not sent to the idle remote), then frees the slot and checks the run
// ended on the local machine.
func stayedLocal(t *testing.T, h *rig, hold *proc, args ...string) string {
	t.Helper()
	p := h.start(args...)
	h.waiting(1)
	time.Sleep(300 * time.Millisecond) // a remote-capable ticket would have been placed by now
	snap := h.snapshot()
	if len(snap.Waiting) != 1 || snap.Waiting[0].Target != "local" || len(snap.Running) != 1 {
		t.Fatalf("want one ticket waiting for local, got %+v", snap)
	}
	h.touch("go")
	if rc := p.wait(t, 20*time.Second); rc != 0 || hold.wait(t, 20*time.Second) != 0 {
		t.Fatalf("rc=%d\n%s", rc, p.out)
	}
	if r := h.lastRecord(); r.Machine != "local" {
		t.Fatalf("ran on %q, want local", r.Machine)
	}
	return p.out.String()
}

func TestE2ENeedGHKeepsTheRunLocal(t *testing.T) {
	h := newRig(t, "")
	h.setConfig(remoteConfig(h, ""))
	gitRepo(t, h)

	rc, out := h.run("3", "--need-gh", "--remote", "box", "--", "true")
	if rc != 2 || !strings.Contains(out, "--need-gh") || !strings.Contains(out, "--remote box") {
		t.Fatalf("with a named remote: rc=%d\n%s", rc, out)
	}
	hold := occupyLocal(h)
	stayedLocal(t, h, hold, "3", "--need-gh", "--remote", "auto", "--", "true")
}

func TestE2ENeedGHReposFromTheConfigAreImplicit(t *testing.T) {
	h := newRig(t, "")
	h.setConfig(remoteConfig(h, "[run]\nneed_gh_repos = [\"app\"]\n"))
	gitRepo(t, h)

	hold := occupyLocal(h)
	out := stayedLocal(t, h, hold, "3", "--", "true")
	if !strings.Contains(out, "app is in [run] need_gh_repos") || strings.Count(out, "need_gh_repos") != 1 {
		t.Fatalf("want one line saying why it runs locally:\n%s", out)
	}
	rc, out := h.run("3", "--remote", "box", "--", "true")
	if rc != 2 || !strings.Contains(out, "need_gh_repos") {
		t.Fatalf("a named remote for a need_gh repo: rc=%d\n%s", rc, out)
	}
}

func TestE2ENeedGHReposConfigIsValidated(t *testing.T) {
	h := newRig(t, "")
	h.setConfig(baseConfig + "[run]\nneed_gh_repos = [\"ok\", \"../x\"]\n")
	if rc, out := h.run("3", "--", "true"); rc != 2 || !strings.Contains(out, "need_gh_repos") {
		t.Fatalf("rc=%d\n%s", rc, out)
	}
}

// siblingGate commits a heavyq.toml whose gate copies the sibling's file into the rig.
func siblingGate(t *testing.T, h *rig, app, extra string) {
	t.Helper()
	toml := fmt.Sprintf("[[gate]]\nname = \"ready\"\nrun = ['cat ../lib/a.txt > %s/seen && echo run >> %s/count-a']\nwith = [\"lib\"]\n%s",
		h.root, h.root, extra)
	if err := os.WriteFile(filepath.Join(app, "heavyq.toml"), []byte(toml), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, h, app, "add", "heavyq.toml")
	gitIn(t, h, app, "commit", "-q", "-m", "gate")
}

func TestE2EGatePassesWithToTheRunAndProofFollowsTheSibling(t *testing.T) {
	h, app, lib := withRig(t, baseConfig)
	siblingGate(t, h, app, "")

	if rc, out := h.run("gate", "run"); rc != 0 || h.runs("a") != 1 || h.read("seen") != "a\n" {
		t.Fatalf("gate run rc=%d runs=%d seen=%q\n%s", rc, h.runs("a"), h.read("seen"), out)
	}
	if rc, g := h.gateJSON(); rc != 0 || !g.Proven {
		t.Fatalf("proven on the pinned sibling: rc=%d %+v", rc, g)
	}
	// Another sibling commit is another run: the old proof does not carry over.
	commitIn(t, h, lib, "a.txt", "newer\n")
	if rc, g := h.gateJSON(); rc != 1 || g.Proven {
		t.Fatalf("a moved sibling still proves the gate: rc=%d %+v", rc, g)
	}
	if rc, out := h.run("gate", "run"); rc != 0 || h.runs("a") != 2 || h.read("seen") != "newer\n" {
		t.Fatalf("second gate run rc=%d runs=%d seen=%q\n%s", rc, h.runs("a"), h.read("seen"), out)
	}
	// A sibling that cannot be resolved proves nothing, whatever the ledger holds.
	if err := os.Rename(lib, lib+"-gone"); err != nil {
		t.Fatal(err)
	}
	if rc, g := h.gateJSON(); rc != 1 || g.Proven {
		t.Fatalf("a missing sibling proves the gate: rc=%d %+v", rc, g)
	}
}

func TestE2EGateNeedGHRunsLocallyDespiteProofRemote(t *testing.T) {
	h := newRig(t, "")
	h.setConfig(remoteConfig(h, "[proof]\nremote = \"box\"\n"))
	app := gitRepo(t, h)
	toml := fmt.Sprintf("[[gate]]\nname = \"ready\"\nrun = ['echo run >> %s/count-a']\nneed_gh = true\n", h.root)
	if err := os.WriteFile(filepath.Join(app, "heavyq.toml"), []byte(toml), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, h, app, "add", "heavyq.toml")
	gitIn(t, h, app, "commit", "-q", "-m", "gate")

	rc, out := h.run("gate", "run")
	if rc != 0 || !strings.Contains(out, "need_gh") {
		t.Fatalf("rc=%d\n%s", rc, out)
	}
	if r := h.lastRecord(); r.Machine != "local" {
		t.Fatalf("ran on %q, want local", r.Machine)
	}
	if rc, out := h.run("gate", "run", "--remote", "box"); rc != 0 {
		t.Fatalf("already proven, nothing to place: rc=%d\n%s", rc, out)
	}
}
