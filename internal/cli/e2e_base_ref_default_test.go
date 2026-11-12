package cli

import (
	"path/filepath"
	"strings"
	"testing"
)

// These tests cover the default of --base-ref (#147): a repository with a remote called origin ships
// origin's default branch to the machine a run goes to, so a command that diffs against origin/main works
// there. They run the real binary on real temp repositories with a bare "origin", and on the fake remote
// "box" (reached through `sh -c`, see remoteConfig).

// addOrigin gives repo a bare "origin" (next to the rig's other repositories) whose default branch is branch:
// main is pushed to it, and refs/remotes/origin/HEAD points at it.
func addOrigin(t *testing.T, h *rig, repo, branch string) {
	t.Helper()
	bare := filepath.Join(h.root, "origin.git")
	gitIn(t, h, h.root, "init", "-q", "--bare", bare)
	gitIn(t, h, repo, "remote", "add", "origin", bare)
	gitIn(t, h, repo, "push", "-q", "origin", "main:"+branch)
	gitIn(t, h, repo, "remote", "set-head", "origin", branch)
}

// moveOriginMain points refs/remotes/origin/main at a new commit with the same tree: what a `git fetch` does
// when origin/main moved on, without changing the code that is checked out.
func moveOriginMain(t *testing.T, h *rig, repo string) {
	t.Helper()
	moved := gitOutput(t, h, repo, "commit-tree", "HEAD^{tree}", "-p", "HEAD", "-m", "origin moved on")
	gitIn(t, h, repo, "update-ref", "refs/remotes/origin/main", moved)
}

// queueConfig is the remote config with one more key in [queue].
func queueConfig(h *rig, line string) string {
	return strings.Replace(remoteConfig(h, ""), "[queue]\n", "[queue]\n"+line+"\n", 1)
}

// remoteRefs is what a command on the fake remote sees under refs/remotes/ for a run with the extra flags.
func remoteRefs(t *testing.T, h *rig, extra ...string) string {
	t.Helper()
	args := append([]string{"3", "--remote", "box"}, extra...)
	rc, out, errOut := h.runSplit(append(args, "--", "git", "for-each-ref", "--format=%(refname)", "refs/remotes")...)
	if rc != 0 {
		t.Fatalf("rc=%d\nstdout: %s\nstderr: %s", rc, out, errOut)
	}
	return strings.TrimSpace(out)
}

func TestE2EBaseRefDefaultsToOriginsDefaultBranchOnARemote(t *testing.T) {
	const onMain, trunk, other = "refs/remotes/origin/main", "refs/remotes/origin/trunk", "refs/remotes/origin/other"
	cases := []struct {
		name  string
		setup func(t *testing.T, h *rig, repo string)
		queue string   // one more line for [queue]
		args  []string // flags of the run
		want  string   // what the remote command sees
	}{
		{"origin/HEAD names main", func(t *testing.T, h *rig, repo string) { addOrigin(t, h, repo, "main") }, "", nil, onMain},
		{"origin/HEAD names another branch", func(t *testing.T, h *rig, repo string) { addOrigin(t, h, repo, "trunk") }, "", nil, trunk},
		{"without origin/HEAD it is origin/main", func(t *testing.T, h *rig, repo string) {
			addOrigin(t, h, repo, "main")
			gitIn(t, h, repo, "remote", "set-head", "origin", "-d")
		}, "", nil, onMain},
		{"no default branch to name, nothing is shipped", func(t *testing.T, h *rig, repo string) {
			addOrigin(t, h, repo, "trunk")
			gitIn(t, h, repo, "remote", "set-head", "origin", "-d")
		}, "", nil, ""},
		{"no origin, nothing is shipped", func(*testing.T, *rig, string) {}, "", nil, ""},
		{"--base-ref none opts out", func(t *testing.T, h *rig, repo string) { addOrigin(t, h, repo, "main") }, "", []string{"--base-ref", "none"}, ""},
		{"queue.base_ref none opts out", func(t *testing.T, h *rig, repo string) { addOrigin(t, h, repo, "main") }, `base_ref = "none"`, nil, ""},
		{"--base-ref wins over the default", func(t *testing.T, h *rig, repo string) {
			addOrigin(t, h, repo, "main")
			gitIn(t, h, repo, "push", "-q", "origin", "main:other")
		}, "", []string{"--base-ref", "origin/other"}, other},
		{"queue.base_ref wins over the default", func(t *testing.T, h *rig, repo string) {
			addOrigin(t, h, repo, "main")
			gitIn(t, h, repo, "push", "-q", "origin", "main:other")
		}, `base_ref = "origin/other"`, nil, other},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := newRig(t, "")
			h.setConfig(queueConfig(h, c.queue))
			repo := gitRepo(t, h)
			c.setup(t, h, repo)
			if got := remoteRefs(t, h, c.args...); got != c.want {
				t.Fatalf("the remote command sees %q under refs/remotes, want %q", got, c.want)
			}
		})
	}
}

// The base commit is part of the run id (so a cached result or a proof never outlives the base it saw), and
// the id is the same wherever the run executes.
func TestE2EBaseRefDefaultIsPartOfTheRunIDWhereverItRuns(t *testing.T) {
	h := newRig(t, "")
	h.setConfig(remoteConfig(h, ""))
	repo := gitRepo(t, h)
	addOrigin(t, h, repo, "main")
	runID := func(args ...string) string {
		t.Helper()
		if rc, out := h.run(append([]string{"3"}, append(args, "--", "true")...)...); rc != 0 {
			t.Fatalf("heavyq %v: rc=%d\n%s", args, rc, out)
		}
		id := h.lastRecord().RunID
		if id == "" {
			t.Fatalf("heavyq %v: the ledger line has no run_id", args)
		}
		return id
	}
	onBox, local := runID("--remote", "box"), runID("--remote", "local")
	if onBox != local {
		t.Fatalf("the run id depends on where it ran: box %s, local %s", onBox, local)
	}
	if none := runID("--remote", "local", "--base-ref", "none"); none == local {
		t.Fatal("opting out of the default base ref must change the run id: the command sees no base")
	}
	moveOriginMain(t, h, repo)
	if moved := runID("--remote", "local"); moved == local {
		t.Fatal("origin/main moved but the run id did not")
	}
}

// `gate run` is proven only when the ledger holds a run with the id `gate status` computes: both must apply
// the same default, on a remote and on this machine.
func TestE2EBaseRefDefaultKeepsAGateProvenWherever(t *testing.T) {
	h := newRig(t, "")
	h.setConfig(remoteConfig(h, ""))
	repo := gateRepo(t, h, "")
	addOrigin(t, h, repo, "main")
	if rc, out := h.run("gate", "run", "ready", "--remote", "box"); rc != 0 {
		t.Fatalf("gate run on the remote: rc=%d\n%s", rc, out)
	}
	if rc, out := h.run("gate", "status", "ready"); rc != 0 {
		t.Fatalf("proven by a remote run, status rc=%d\n%s", rc, out)
	}
	moveOriginMain(t, h, repo)
	if rc, out := h.run("gate", "status", "ready"); rc != 1 {
		t.Fatalf("origin/main moved, so the proof is stale: status rc=%d, want 1\n%s", rc, out)
	}
	if rc, out := h.run("gate", "run", "ready", "--remote", "local"); rc != 0 {
		t.Fatalf("gate run here: rc=%d\n%s", rc, out)
	}
	if rc, out := h.run("gate", "status", "ready"); rc != 0 {
		t.Fatalf("proven by a local run, status rc=%d\n%s", rc, out)
	}
}
