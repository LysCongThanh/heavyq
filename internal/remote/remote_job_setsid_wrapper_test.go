package remote

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A field incident: a remote had a program named setsid first on its non-login PATH that forked and exited 0
// in the parent. The job script ran `setsid bash -c "$CMD" &` and waited for that pid, so every run there
// "passed" (false and exit 3 included) and its worktree was removed while the real command still ran.

// forkingSetsid forks the command and exits 0 at once, like the wrapper of the incident.
const forkingSetsid = `"$@" &
exit 0`

// lyingSetsid is forkingSetsid that also claims to be util-linux's when asked for its version.
const lyingSetsid = `if [ "$1" = --version ]; then echo "setsid from util-linux 2.39.3"; exit 0; fi
"$@" &
exit 0`

// wrapperDir makes a directory holding an executable setsid with body (a /bin/sh script).
func wrapperDir(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "setsid"), []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

// fileAppears waits up to d for path to exist.
func fileAppears(path string, d time.Duration) bool {
	for deadline := time.Now().Add(d); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		if _, err := os.Stat(path); err == nil {
			return true
		}
	}
	return false
}

// The remote's env_path puts the wrapper first, as the remote's PATH did in the incident. The job must still
// return the command's own exit code, and only once the command has finished.
func TestJobKeepsExitCodeWhenSetsidIsAForkingWrapper(t *testing.T) {
	repo, sha := pushApp(t)
	m := fakeRemote(t)
	m.EnvPath = wrapperDir(t, forkingSetsid)
	if err := Push(m, repo, "app", sha, sha[:12], 20*time.Second); err != nil {
		t.Fatal(err)
	}
	done := filepath.Join(m.WorkDir, "cache", "finished")
	rc, out, errs := runJob(t, JobSpec{Machine: m, Repo: "app", SHA: sha, Lane: "x", Slot: "h1",
		Argv: []string{"sh", "-c", `sleep 0.3; echo done > "$HEAVYQ_CACHE_DIR/finished"; exit 3`}})
	_, statErr := os.Stat(done)
	if rc != 3 || statErr != nil {
		fileAppears(done, 3*time.Second) // let a stray command finish before the temp dir goes
		t.Fatalf("rc=%d (want 3), command finished before the job returned: %v\nstdout:%s\nstderr:%s", rc, statErr == nil, out, errs)
	}
	assertNoWorktrees(t, m)
}
