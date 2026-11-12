package remote

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Jobs of one repository share its bare repository on the remote, but each
// has a repository of its own and with it refs/remotes/<base>. With the base
// ref on by default, two runs of one repo meet different bases whenever a fetch
// lands between them: the second job runs at once with its own base, and the
// first, still running, keeps seeing the base it was given.
func TestRemoteJobsOfOneRepoRunSideBySideEachWithItsOwnBase(t *testing.T) {
	src := t.TempDir()
	repo, x := newRepo(t, src, "app", "f", "x")
	if err := os.WriteFile(filepath.Join(repo, "f"), []byte("y"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, repo, "commit", "-q", "-am", "y")
	y := gitRun(t, repo, "rev-parse", "HEAD")
	m := fakeRemote(t)
	m.HeavySlots = 2
	for _, c := range []string{x, y} {
		if err := Push(m, repo, "app", c, c[:12], 20*time.Second); err != nil {
			t.Fatal(err)
		}
	}
	gate := filepath.Join(src, "go")
	var aOut, aErr syncBuffer
	a, err := StartJob(JobSpec{Machine: m, Repo: "app", SHA: x, Lane: "a", Slot: "h1", BaseRef: "origin/main", BaseSHA: x,
		Argv:   []string{"sh", "-c", "git rev-parse origin/main; while [ ! -e " + gate + " ]; do sleep 0.02; done; git rev-parse origin/main"},
		Stdout: &aOut, Stderr: &aErr})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(20 * time.Second)
	for !strings.Contains(aOut.String(), x) {
		if time.Now().After(deadline) {
			t.Fatalf("job a never started:\n%s", aErr.String())
		}
		time.Sleep(20 * time.Millisecond)
	}
	rc, out, errs := runJob(t, JobSpec{Machine: m, Repo: "app", SHA: y, Lane: "b", Slot: "h2", BaseRef: "origin/main", BaseSHA: y,
		Argv: []string{"git", "rev-parse", "origin/main"}})
	if err := os.WriteFile(gate, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if rcA := a.Wait(); rcA != 0 || strings.Count(aOut.String(), x) != 2 {
		t.Fatalf("job a's base moved under it (rc=%d):\n%s\n%s\njob b: rc=%d %s %s", rcA, aOut.String(), aErr.String(), rc, out, errs)
	}
	if rc != 0 || strings.TrimSpace(out) != y {
		t.Fatalf("job b did not run with its own base %s: rc=%d\n%s\n%s", y[:12], rc, out, errs)
	}
	assertNoWorktrees(t, m)
}
