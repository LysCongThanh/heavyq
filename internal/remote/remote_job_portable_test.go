package remote

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LysCongThanh/heavyq/internal/slots"
)

// A stock Mac has neither flock(1) nor setsid(1). These tests run the real job
// script on this machine with a PATH from which both are removed, which sends
// it down the Perl branches macOS takes; the real thing is covered by the
// HEAVYQ_TEST_MAC_REMOTE tests.

// pathWithout points PATH at a directory of symlinks to every command on the
// current PATH except the named ones.
func pathWithout(t *testing.T, names ...string) {
	t.Helper()
	skip := map[string]bool{}
	for _, n := range names {
		skip[n] = true
	}
	farm := t.TempDir()
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		ents, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range ents {
			link := filepath.Join(farm, e.Name())
			if _, err := os.Lstat(link); skip[e.Name()] || err == nil {
				continue
			}
			_ = os.Symlink(filepath.Join(dir, e.Name()), link)
		}
	}
	t.Setenv("PATH", farm)
	for _, n := range names {
		if p, err := exec.LookPath(n); err == nil {
			t.Fatalf("%s still on PATH at %s", n, p)
		}
	}
	if _, err := exec.LookPath("perl"); err != nil {
		t.Skip("perl is required for the portable remote path")
	}
}

// stubDir creates executables (name -> shell body) and puts the directory first on PATH.
func stubDir(t *testing.T, stubs map[string]string) {
	t.Helper()
	dir := t.TempDir()
	for name, body := range stubs {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// marker returns a command-line token unique to one test run and a pgrep
// pattern for it that does not match pgrep's own command line.
func marker() (token, pattern string) {
	token = fmt.Sprintf("3600.%d", time.Now().UnixNano()%1_000_000)
	return token, "sleep " + token[:len(token)-1] + "[" + token[len(token)-1:] + "]"
}

func pushApp(t *testing.T) (string, string) {
	t.Helper()
	repo, sha := newRepo(t, t.TempDir(), "app", "f", "x")
	return repo, sha
}

func TestPortableJobRunsInOwnProcessGroupWithoutSetsid(t *testing.T) {
	repo, sha := pushApp(t)
	m := fakeRemote(t)
	if err := Push(m, repo, "app", sha, sha[:12], 20*time.Second); err != nil {
		t.Fatal(err)
	}
	pathWithout(t, "flock", "setsid")
	rc, out, errs := runJob(t, JobSpec{Machine: m, Repo: "app", SHA: sha, Lane: "x", Slot: "h1",
		Argv: []string{"bash", "-c", `echo "$$ $(ps -o pgid= -p $$ | tr -d ' ')"; exit 3`}})
	f := strings.Fields(out)
	if rc != 3 || len(f) != 2 || f[0] != f[1] {
		t.Fatalf("rc=%d, want its own process group (pid pgid): %q\nstderr:%s", rc, out, errs)
	}
	assertNoWorktrees(t, m)
}

func TestPortableJobSlotBusyAndLockReleasedAfterRun(t *testing.T) {
	repo, sha := pushApp(t)
	m := fakeRemote(t)
	if err := Push(m, repo, "app", sha, sha[:12], 20*time.Second); err != nil {
		t.Fatal(err)
	}
	pathWithout(t, "flock", "setsid")
	l, err := slots.TryLock(filepath.Join(m.WorkDir, "slots", "h1.lock"))
	if err != nil || l == nil {
		t.Fatal("cannot hold remote slot", err)
	}
	start := time.Now()
	rc, _, errs := runJob(t, JobSpec{Machine: m, Repo: "app", SHA: sha, Lane: "x", Slot: "h1", Argv: []string{"true"}})
	l.Release()
	if rc != ExitSlotBusy || !strings.Contains(errs, "busy") || time.Since(start) > 15*time.Second {
		t.Fatalf("busy slot: rc=%d after %s: %s", rc, time.Since(start), errs)
	}
	// the Perl lock is a real flock(2): it is gone once the job ends, so the slot is free again
	if rc, _, errs := runJob(t, JobSpec{Machine: m, Repo: "app", SHA: sha, Lane: "x", Slot: "h1", Argv: []string{"true"}}); rc != 0 {
		t.Fatalf("slot not free after a portable run: rc=%d %s", rc, errs)
	}
	l, err = slots.TryLock(filepath.Join(m.WorkDir, "slots", "h1.lock"))
	if err != nil || l == nil {
		t.Fatalf("slot still locked after the job: %v", err)
	}
	l.Release()
}

func TestPortableJobTeardownWhenLifelineCloses(t *testing.T) {
	repo, sha := pushApp(t)
	m := fakeRemote(t)
	if err := Push(m, repo, "app", sha, sha[:12], 20*time.Second); err != nil {
		t.Fatal(err)
	}
	pathWithout(t, "flock", "setsid")
	token, pattern := marker()
	var out syncBuffer
	j, err := StartJob(JobSpec{Machine: m, Repo: "app", SHA: sha, Lane: "x", Slot: "h1",
		Argv: []string{"sh", "-c", "echo started; sleep " + token}, Stdout: &out, Stderr: &out})
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
	if err := exec.Command("pgrep", "-f", pattern).Run(); err == nil {
		t.Fatal("the command survived the teardown")
	}
	assertNoWorktrees(t, m)
}
