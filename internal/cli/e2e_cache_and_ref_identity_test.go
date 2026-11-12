package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The cache key is computed before the queue wait. A tree edited during the
// wait (A -> B) and restored while the command runs (B -> A) gives the same
// key at the end, but the command ran on B: its result must not be stored
// under A's key and replayed later as A's.
func TestE2ECacheNeverStoresAResultOfAnotherTree(t *testing.T) {
	h := newRig(t, baseConfig)
	repo := gitRepo(t, h)
	if err := os.WriteFile(filepath.Join(repo, "a.txt"), []byte("bad\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, h, repo, "add", "a.txt")
	gitIn(t, h, repo, "commit", "-q", "-m", "a")
	cacheEditedAndRestored(t, h, repo, "a.txt")
}

// The same for a run that proves a gate with paths: its checkout is recorded on the scoped id, while the key and
// a replay are on the whole tree. An edit inside the paths and one outside them both make the result B's.
func TestE2ECacheNeverStoresAScopedGateResultOfAnotherTree(t *testing.T) {
	for _, file := range []string{"a.txt", "b.txt"} {
		t.Run(file, func(t *testing.T) {
			h := newRig(t, baseConfig)
			app, _ := itemRepo(t, h, "# Goal: scope\n", `paths = ["a.txt"]`, map[string]string{"a.txt": "bad\n", "b.txt": "bad\n"})
			cacheEditedAndRestored(t, h, app, file, "--gate", "web")
		})
	}
}

// cacheEditedAndRestored runs a --cache leader whose command passes only when file says good, edits file to good
// while the leader waits in the queue and restores it while the command runs. A later identical run on the
// committed tree must run for real and fail.
func cacheEditedAndRestored(t *testing.T, h *rig, repo, file string, extra ...string) {
	t.Helper()
	blocker := h.start("3", "--lane", "blocker", "--", "sh", "-c", h.gateFile("release"))
	h.running(1)
	cmd := "grep -q good " + file + "; rc=$?; touch " + filepath.Join(h.root, "started") + "; " + h.gateFile("reverted") + "; exit $rc"
	args := func(lane string) []string {
		return append(append([]string{"5", "--cache", "--lane", lane}, extra...), "--", "sh", "-c", cmd)
	}
	leader := h.start(args("leader")...)
	h.waiting(1)
	if err := os.WriteFile(filepath.Join(repo, file), []byte("good\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	h.touch("release")
	if rc := blocker.wait(t, 30*time.Second); rc != 0 {
		t.Fatalf("blocker rc=%d\n%s", rc, blocker.out)
	}
	h.until("the leader's command to start", func() bool { _, err := os.Stat(filepath.Join(h.root, "started")); return err == nil })
	gitIn(t, h, repo, "checkout", "--", file)
	h.touch("reverted")
	if rc := leader.wait(t, 30*time.Second); rc != 0 {
		t.Fatalf("the leader ran on the edited tree and passed: rc=%d\n%s", rc, leader.out)
	}
	rc, out := h.run(args("later")...)
	if rc != 1 || strings.Contains(out, "cached result") {
		t.Fatalf("on the committed tree the command fails; a replay of the edited tree's pass is a lie: rc=%d\n%s", rc, out)
	}
}

// Default placement with --ref other than HEAD can only go remote (local runs
// in place, on HEAD), and must ship the commit asked for, not HEAD.
func TestE2EDefaultPlacementRunsTheRequestedRef(t *testing.T) {
	h := newRig(t, "")
	h.setConfig(remoteConfig(h, ""))
	repo := gitRepo(t, h)
	write := func(v string) {
		if err := os.WriteFile(filepath.Join(repo, "v.txt"), []byte(v+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		gitIn(t, h, repo, "add", "v.txt")
		gitIn(t, h, repo, "commit", "-q", "-m", v)
	}
	write("v1")
	gitIn(t, h, repo, "tag", "v1")
	write("v2")
	rc, out := h.run("5", "--ref", "v1", "--", "cat", "v.txt")
	if rc != 0 || !strings.Contains(out, "v1") || strings.Contains(out, "v2") {
		t.Fatalf("--ref v1 must run v1: rc=%d\n%s", rc, out)
	}
	if r := h.lastRecord(); r.Machine != "box" || r.Commit != gitOutput(t, h, repo, "rev-parse", "v1^{commit}") {
		t.Fatalf("the ledger must name the commit that ran: %+v", r)
	}
}
