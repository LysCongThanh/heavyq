package remote

import (
	"path/filepath"
	"strings"
	"testing"
)

// Each job runs in a repository of its own that borrows the bare repo's
// objects, so the bare repo's core.hooksPath=/dev/null does not reach it.
// The job script still runs git there (the base ref is created with
// update-ref), and a reference-transaction hook of the receiving machine that
// declines would fail the ship (exit 77) or print into the job's output.
func TestJobCheckoutRunsNoHookOfTheReceivingMachine(t *testing.T) {
	repo, x := newRepo(t, t.TempDir(), "app", "f", "x")
	y := commitFile(t, repo, "f", "y")
	hooksDir := filepath.Join(t.TempDir(), "global-hooks")
	logFile := failingHooks(t, hooksDir, receivingHooks...)
	globalHooksPath(t, hooksDir) // after the commits: only the ship and the job run under these hooks
	m := fakeRemote(t)
	mustPush(t, m, repo, "app", y, y[:12])
	mustPush(t, m, repo, "app", x, "base-"+x[:12])
	rc, out, errs := runJob(t, JobSpec{Machine: m, Repo: "app", SHA: y, Lane: "x", Slot: "h1", BaseRef: "origin/main", BaseSHA: x,
		Argv: []string{"git", "rev-parse", "origin/main"}})
	if rc != 0 || strings.TrimSpace(out) != x {
		t.Fatalf("a hook of the receiving machine must not stop the job: rc=%d out=%q err=%q", rc, out, errs)
	}
	if strings.Contains(out+errs, "stdout of") {
		t.Fatalf("hook output reached the job:\nstdout:%q\nstderr:%q", out, errs)
	}
	assertNoHookRan(t, logFile)
}
