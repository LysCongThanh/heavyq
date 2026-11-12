package remote

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LysCongThanh/heavyq/internal/config"
	"github.com/LysCongThanh/heavyq/internal/slots"
)

func shippedApp(t *testing.T) (config.MachineConfig, string) {
	t.Helper()
	repo, sha := newRepo(t, t.TempDir(), "app", "f", "x")
	m := fakeRemote(t)
	mustPush(t, m, repo, "app", sha, sha[:12])
	return m, sha
}

// ssh exits 255 when the connection drops, so a command's own 255 used to read as a lost connection: the run
// counted as not run and an agent was told to retry a deterministic failure. Like 97/98, it becomes 96.
func TestRemoteJobCommandsOwn255Becomes96(t *testing.T) {
	m, sha := shippedApp(t)
	rc, _, errs := runJob(t, JobSpec{Machine: m, Repo: "app", SHA: sha, Lane: "x", Slot: "h1", Argv: []string{"sh", "-c", "exit 255"}})
	if rc != 96 || !strings.Contains(errs, "255") {
		t.Fatalf("command's own 255: rc=%d, want 96 and a line naming 255\n%s", rc, errs)
	}
}

// Locally argv is executed as it is; remotely the first word used to be parsed by bash, so `time` became the
// keyword and FOO=1 an assignment. The first word is now always a command name, as it is locally.
func TestRemoteJobFirstWordIsACommandNameAsLocally(t *testing.T) {
	m, sha := shippedApp(t)
	for _, argv := range [][]string{{"FOO=1", "env"}, {"if", "true"}} {
		const want = 127 // what exec of that name gives locally
		if rc, out, errs := runJob(t, JobSpec{Machine: m, Repo: "app", SHA: sha, Lane: "x", Slot: "h1", Argv: argv}); rc != want {
			t.Fatalf("%q: rc=%d, want %d as locally\nstdout:%s\nstderr:%s", argv, rc, want, out, errs)
		}
	}
}

// The command used to travel as one exec argument (base64 in the env of the transport's command line), so
// an argv over about 96 KB that runs locally failed remotely with "argument list too long" (exit 77).
func TestRemoteJobRunsAnArgvLargerThanOneExecArgument(t *testing.T) {
	m, sha := shippedApp(t)
	argv := []string{"sh", "-c", `echo "$#"`, "sh"}
	for i := 0; i < 3000; i++ {
		argv = append(argv, fmt.Sprintf("packages/some-workspace/src/components/file-%04d.ts", i))
	}
	rc, out, errs := runJob(t, JobSpec{Machine: m, Repo: "app", SHA: sha, Lane: "x", Slot: "h1", Argv: argv})
	if rc != 0 || strings.TrimSpace(out) != "3000" {
		t.Fatalf("rc=%d out=%q\n%s", rc, out, errs)
	}
	assertNoWorktrees(t, m)
}

// A stop request (heavyq cancelled, connection dropped) during the cache hook used to wait for the hook to
// finish, with the remote slot held all along. The hook now stops at once and the slot is free.
func TestRemoteJobStopsDuringTheCacheHookAndFreesTheSlot(t *testing.T) {
	m, sha := shippedApp(t)
	token, pattern := marker()
	m.CacheHook = "echo hook-started; sleep " + token
	var out syncBuffer
	j, err := StartJob(JobSpec{Machine: m, Repo: "app", SHA: sha, Lane: "x", Slot: "h1", Argv: []string{"true"}, Stdout: &out, Stderr: &out})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = exec.Command("pkill", "-KILL", "-f", pattern).Run() })
	waitFor(t, &out, "hook-started")
	j.lifeline.Close()
	done := make(chan int, 1)
	go func() { done <- j.Wait() }()
	select {
	case rc := <-done:
		if rc != 143 {
			t.Fatalf("rc = %d, want 143\n%s", rc, out.String())
		}
	case <-time.After(10 * time.Second):
		_ = exec.Command("pkill", "-KILL", "-f", pattern).Run()
		t.Fatal("the job waited for the cache hook after its lifeline closed")
	}
	if err := exec.Command("pgrep", "-f", pattern).Run(); err == nil {
		t.Fatal("the cache hook survived the teardown")
	}
	l, err := slots.TryLock(filepath.Join(m.WorkDir, "slots", "h1.lock"))
	if err != nil || l == nil {
		t.Fatalf("the remote slot is still held after the teardown: %v", err)
	}
	l.Release()
	if _, err := os.Stat(filepath.Join(m.WorkDir, "wt")); err == nil {
		assertNoWorktrees(t, m)
	}
}
