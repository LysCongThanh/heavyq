package remote

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LysCongThanh/heavyq/internal/config"
	"github.com/LysCongThanh/heavyq/internal/slots"
)

// The "remote" here is the local machine reached through `sh -c`: the exact
// code path ssh would take, minus the network.

func gitRun(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// newRepo creates a repo with one committed file and returns (path, sha).
func newRepo(t *testing.T, parent, name, file, content string) (string, string) {
	t.Helper()
	dir := filepath.Join(parent, name)
	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "init", "-q", "-b", "main")
	for _, f := range []string{file, "sub/.keep"} {
		if err := os.WriteFile(filepath.Join(dir, f), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-q", "-m", "init")
	return dir, gitRun(t, dir, "rev-parse", "HEAD")
}

func fakeRemote(t *testing.T) config.MachineConfig {
	work := filepath.Join(t.TempDir(), "work")
	return config.MachineConfig{Name: "box", HeavySlots: 1, LightSlots: 1, Transport: []string{"sh", "-c"},
		WorkDir: work, GitURL: filepath.Join(work, "git")}
}

func runJob(t *testing.T, spec JobSpec) (int, string, string) {
	t.Helper()
	var out, errb bytes.Buffer
	spec.Stdout, spec.Stderr = &out, &errb
	j, err := StartJob(spec)
	if err != nil {
		t.Fatal(err)
	}
	return j.Wait(), out.String(), errb.String()
}

func assertNoWorktrees(t *testing.T, m config.MachineConfig) {
	t.Helper()
	ents, _ := os.ReadDir(filepath.Join(m.WorkDir, "wt"))
	if len(ents) != 0 {
		t.Fatalf("worktrees left behind: %v", ents)
	}
}

func TestRemoteJobExactCommitSiblingsBaseRefAndHook(t *testing.T) {
	src := t.TempDir()
	repo, sha := newRepo(t, src, "app", "marker.txt", "committed")
	lib, libSHA := newRepo(t, src, "lib", "lib.txt", "sibling")
	gitRun(t, repo, "update-ref", "refs/remotes/origin/main", sha)
	// uncommitted edits must NOT travel
	if err := os.WriteFile(filepath.Join(repo, "marker.txt"), []byte("dirty"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := fakeRemote(t)
	m.CacheHook = `touch "$HEAVYQ_CACHE_DIR/hook-ran"`
	for _, p := range [][3]string{{repo, "app", sha}, {lib, "lib", libSHA}} {
		if err := Push(m, p[0], p[1], p[2], p[2][:12], 20*time.Second); err != nil {
			t.Fatal(err)
		}
	}
	rc, out, errs := runJob(t, JobSpec{Machine: m, Repo: "app", SHA: sha, Lane: "api", Slot: "h1", Subdir: "sub",
		Argv:     []string{"sh", "-c", `cat ../marker.txt; echo; cat ../../lib/lib.txt; echo; git rev-parse origin/main; test -f "$HEAVYQ_CACHE_DIR/hook-ran" && echo hook; basename "$PWD"; exit 7`},
		Siblings: []string{"lib@" + libSHA}, BaseRef: "origin/main", BaseSHA: sha})
	if rc != 7 {
		t.Fatalf("rc = %d, want 7\nstdout:%s\nstderr:%s", rc, out, errs)
	}
	for _, want := range []string{"committed", "sibling", sha, "hook", "sub"} {
		if !strings.Contains(out, want) {
			t.Fatalf("stdout missing %q:\n%s\nstderr:%s", want, out, errs)
		}
	}
	assertNoWorktrees(t, m)
}

func TestRemoteJobReservedCodesAndBusySlot(t *testing.T) {
	repo, sha := newRepo(t, t.TempDir(), "app", "f", "x")
	m := fakeRemote(t)
	if err := Push(m, repo, "app", sha, sha[:12], 20*time.Second); err != nil {
		t.Fatal(err)
	}
	if rc, _, _ := runJob(t, JobSpec{Machine: m, Repo: "app", SHA: sha, Lane: "x", Slot: "h1", Argv: []string{"sh", "-c", "exit 97"}}); rc != 96 {
		t.Fatalf("command's own 97 must become 96, got %d", rc)
	}
	if rc, _, _ := runJob(t, JobSpec{Machine: m, Repo: "app", SHA: sha, Lane: "x", Slot: "h1", Argv: []string{"sh", "-c", "exit 255"}}); rc != 96 {
		t.Fatalf("command's own 255 must become 96 (255 is the transport's connection lost), got %d", rc)
	}
	l, err := slots.TryLock(filepath.Join(m.WorkDir, "slots", "h1.lock"))
	if err != nil || l == nil {
		t.Fatal("cannot hold remote slot", err)
	}
	res, err := Probe(m, 10*time.Second)
	if err != nil || !res.Busy["h1"] || res.Busy["l1"] || !res.Metrics.Known {
		t.Fatalf("probe = %+v, %v", res, err)
	}
	start := time.Now()
	rc, _, _ := runJob(t, JobSpec{Machine: m, Repo: "app", SHA: sha, Lane: "x", Slot: "h1", Argv: []string{"true"}})
	l.Release()
	if rc != ExitSlotBusy || time.Since(start) > 15*time.Second {
		t.Fatalf("busy slot: rc=%d after %s", rc, time.Since(start))
	}
	if rc, _, _ := runJob(t, JobSpec{Machine: m, Repo: "app", SHA: strings.Repeat("a", 40), Lane: "x", Slot: "h1", Argv: []string{"true"}}); rc != ExitSetupFailed {
		t.Fatalf("unknown commit must be a setup failure, got %d", rc)
	}
}

// When heavyq dies its end of the lifeline closes; the remote side must stop
// the command and clean up on its own.
func TestRemoteJobStopsWhenLifelineCloses(t *testing.T) {
	repo, sha := newRepo(t, t.TempDir(), "app", "f", "x")
	m := fakeRemote(t)
	if err := Push(m, repo, "app", sha, sha[:12], 20*time.Second); err != nil {
		t.Fatal(err)
	}
	var out syncBuffer
	j, err := StartJob(JobSpec{Machine: m, Repo: "app", SHA: sha, Lane: "x", Slot: "h1",
		Argv: []string{"sh", "-c", "echo started; sleep 60"}, Stdout: &out, Stderr: &out})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(15 * time.Second)
	for !strings.Contains(out.String(), "started") && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	j.lifeline.Close()
	done := make(chan int, 1)
	go func() { done <- j.Wait() }()
	select {
	case rc := <-done:
		if rc != 143 {
			t.Fatalf("rc = %d, want 143", rc)
		}
	case <-time.After(15 * time.Second):
		j.Signal(os.Kill)
		t.Fatal("job did not stop after the lifeline closed")
	}
	assertNoWorktrees(t, m)
}
