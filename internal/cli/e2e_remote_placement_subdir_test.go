package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Default placement goes remote only when that is safe. A working directory a
// remote run cannot use (a name outside the safe set, or a directory that is
// not in the commit, such as an ignored build/) keeps the run local, instead of
// failing a run the user never asked to ship.
func TestE2EDefaultPlacementStaysLocalInADirectoryARemoteCannotUse(t *testing.T) {
	h := newRig(t, "")
	h.setConfig(remoteConfig(h, ""))
	repo := gitRepo(t, h)
	spaced := filepath.Join(repo, "my dir")
	if err := os.MkdirAll(spaced, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(spaced, "f.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".gitignore"), []byte("build/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, h, repo, "add", ".")
	gitIn(t, h, repo, "commit", "-q", "-m", "dirs")
	h.cwd = spaced
	if rc, out := h.run("5", "--", "true"); rc != 0 {
		t.Fatalf("a run from %q: rc=%d\n%s", "my dir", rc, out)
	}

	build := filepath.Join(repo, "build")
	if err := os.MkdirAll(build, 0o755); err != nil {
		t.Fatal(err)
	}
	h.cwd = repo
	hold := occupyLocal(h) // the remote is idle: a ticket that may go there does
	h.cwd = build
	p := h.start("5", "--", "pwd")
	h.waiting(1)
	time.Sleep(300 * time.Millisecond)
	if s := h.snapshot(); len(s.Waiting) != 1 || s.Waiting[0].Target != "local" {
		t.Fatalf("a run from an ignored directory must wait for local: %+v", s)
	}
	h.touch("go")
	if rc := p.wait(t, 20*time.Second); rc != 0 || hold.wait(t, 20*time.Second) != 0 || !strings.Contains(p.out.String(), "/build") {
		t.Fatalf("rc=%d\n%s", rc, p.out)
	}
}

// --repo naming a subdirectory, from a directory outside it: a local run runs
// there, so a remote run must too, and both record that subdirectory.
func TestE2ERemoteRunUsesTheRepoSubdirectory(t *testing.T) {
	h := newRig(t, "")
	h.setConfig(remoteConfig(h, ""))
	repo := gitRepo(t, h)
	sub := filepath.Join(repo, "services", "api")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "here.txt"), []byte("api\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, h, repo, "add", ".")
	gitIn(t, h, repo, "commit", "-q", "-m", "sub")
	h.cwd = h.root
	rc, out := h.run("5", "--remote", "box", "--repo", sub, "--", "cat", "here.txt")
	if rc != 0 || !strings.Contains(out, "api") {
		t.Fatalf("the remote must run in services/api: rc=%d\n%s", rc, out)
	}
	if r := h.lastRecord(); r.Subdir != "services/api" || r.Machine != "box" {
		t.Fatalf("%+v", r)
	}
}

// The base a remote run records is the one its job was given, not the local
// ref as it is when the run ends: a fetch during the run must not change it.
func TestE2ERemoteRunRecordsTheBaseItWasGiven(t *testing.T) {
	h := newRig(t, "")
	h.setConfig(remoteConfig(h, ""))
	repo := gitRepo(t, h)
	x := gitOutput(t, h, repo, "rev-parse", "HEAD")
	gitIn(t, h, repo, "update-ref", "refs/remotes/origin/main", x)
	y := commitIn(t, h, repo, "next.txt", "y\n")
	gitIn(t, h, repo, "checkout", "-q", "--detach", x)
	argv := []string{"sh", "-c", "git rev-parse origin/main; " + h.gateFile("go")}
	p := h.start(append([]string{"5", "--remote", "box", "--base-ref", "origin/main", "--"}, argv...)...)
	h.running(1)
	gitIn(t, h, repo, "update-ref", "refs/remotes/origin/main", y) // a fetch while the job runs
	h.touch("go")
	if rc := p.wait(t, 30*time.Second); rc != 0 || !strings.Contains(p.out.String(), x) {
		t.Fatalf("the job must see the base it was given: rc=%d\n%s", rc, p.out)
	}
	remoteRun := h.lastRecord()
	gitIn(t, h, repo, "update-ref", "refs/remotes/origin/main", x)
	if rc, out := h.run(append([]string{"5", "--remote", "local", "--base-ref", "origin/main", "--"}, argv...)...); rc != 0 {
		t.Fatalf("rc=%d\n%s", rc, out)
	}
	if local := h.lastRecord(); remoteRun.RunID == "" || remoteRun.RunID != local.RunID {
		t.Fatalf("the remote run must be recorded with base %.12s, like a run that saw it: remote %s, local %s", x, remoteRun.RunID, local.RunID)
	}
}
