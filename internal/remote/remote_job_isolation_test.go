package remote

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/LysCongThanh/heavyq/internal/config"
)

// commitFile commits content to file in repo and returns the new commit; env adds to the commit's environment.
func commitFile(t *testing.T, repo, file, content string, env ...string) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(repo, file), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", "."}, {"commit", "-q", "-m", content}} {
		cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
		cmd.Env = append(os.Environ(), append([]string{"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com"}, env...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return gitRun(t, repo, "rev-parse", "HEAD")
}

func mustPush(t *testing.T, m config.MachineConfig, repo, name, sha, ref string) {
	t.Helper()
	if err := Push(m, repo, name, sha, ref, 20*time.Second); err != nil {
		t.Fatal(err)
	}
}

// waitFor polls buf for want for up to 15 s.
func waitFor(t *testing.T, buf *syncBuffer, want string) {
	t.Helper()
	for deadline := time.Now().Add(15 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if strings.Contains(buf.String(), want) {
			return
		}
	}
	t.Fatalf("never saw %q:\n%s", want, buf.String())
}

// Every job of a repo used to share one refs/remotes/<base_ref> in the bare repo: a job started later moved
// it under a running one, and depending on the order it was left behind for runs that asked for no base.
func TestConcurrentJobsEachSeeTheirOwnBaseRef(t *testing.T) {
	repo, x := newRepo(t, t.TempDir(), "app", "f", "x")
	y := commitFile(t, repo, "f", "y")
	m := fakeRemote(t)
	mustPush(t, m, repo, "app", y, y[:12])
	mustPush(t, m, repo, "app", x, "base-"+x[:12])
	gate := filepath.Join(t.TempDir(), "go")

	var outA syncBuffer
	a, err := StartJob(JobSpec{Machine: m, Repo: "app", SHA: y, Lane: "x", Slot: "h1", BaseRef: "origin/main", BaseSHA: x,
		Argv:   []string{"sh", "-c", `echo started; while [ ! -e "$1" ]; do sleep 0.05; done; echo "base=$(git rev-parse origin/main)"`, "sh", gate},
		Stdout: &outA, Stderr: &outA})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, &outA, "started")
	rc, out, errs := runJob(t, JobSpec{Machine: m, Repo: "app", SHA: y, Lane: "x", Slot: "h2", BaseRef: "origin/main", BaseSHA: y,
		Argv: []string{"git", "rev-parse", "origin/main"}})
	if rc != 0 || strings.TrimSpace(out) != y {
		t.Fatalf("second job: rc=%d out=%q\n%s", rc, out, errs)
	}
	if err := os.WriteFile(gate, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if rc := a.Wait(); rc != 0 || !strings.Contains(outA.String(), "base="+x) {
		t.Fatalf("the running job's base ref moved under it: rc=%d\n%s", rc, outA.String())
	}
	rc, out, errs = runJob(t, JobSpec{Machine: m, Repo: "app", SHA: y, Lane: "x", Slot: "h1",
		Argv: []string{"sh", "-c", "git rev-parse -q --verify origin/main || echo no-base"}})
	if rc != 0 || strings.TrimSpace(out) != "no-base" {
		t.Fatalf("a job without --base-ref sees a base left behind: rc=%d out=%q\n%s", rc, out, errs)
	}
	assertNoWorktrees(t, m)
}

// A job whose script dies without its EXIT trap (SIGKILL, a reboot of the remote) used to leave its checkout
// under work_dir forever. The next job on the machine reclaims it.
func TestNextJobReclaimsTheCheckoutOfAKilledJob(t *testing.T) {
	repo, sha := newRepo(t, t.TempDir(), "app", "f", "x")
	m := fakeRemote(t)
	mustPush(t, m, repo, "app", sha, sha[:12])
	token, pattern := marker()
	var out syncBuffer
	j, err := StartJob(JobSpec{Machine: m, Repo: "app", SHA: sha, Lane: "x", Slot: "h1",
		Argv: []string{"sh", "-c", `echo "job=$HEAVYQ_JOB_ID"; sleep ` + token}, Stdout: &out, Stderr: &out})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, &out, "job=")
	id := strings.TrimSpace(strings.SplitN(strings.SplitN(out.String(), "job=", 2)[1], "\n", 2)[0])
	pid, err := strconv.Atoi(id[strings.LastIndex(id, "-")+1:])
	if err != nil {
		t.Fatalf("job id %q: %v", id, err)
	}
	_ = syscall.Kill(pid, syscall.SIGKILL) // the job script dies without running its cleanup
	_ = exec.Command("pkill", "-KILL", "-f", pattern).Run()
	j.Wait()
	if ents, _ := os.ReadDir(filepath.Join(m.WorkDir, "wt")); len(ents) == 0 {
		t.Fatal("the killed job left nothing behind; the test does not test anything")
	}
	if rc, _, errs := runJob(t, JobSpec{Machine: m, Repo: "app", SHA: sha, Lane: "x", Slot: "h1", Argv: []string{"true"}}); rc != 0 {
		t.Fatalf("rc=%d\n%s", rc, errs)
	}
	assertNoWorktrees(t, m)
}

// refs/heavyq/<sha12> keeps every shipped commit reachable; a job drops those of commits older than a month
// (a later run of such a commit ships it again) and keeps recent ones, which make the next push small.
func TestJobsPruneRefsOfOldShippedCommits(t *testing.T) {
	repo, _ := newRepo(t, t.TempDir(), "app", "f", "x")
	old := commitFile(t, repo, "f", "old", "GIT_COMMITTER_DATE="+time.Now().AddDate(0, 0, -90).Format(time.RFC3339))
	recent := commitFile(t, repo, "f", "recent")
	m := fakeRemote(t)
	mustPush(t, m, repo, "app", old, old[:12])
	mustPush(t, m, repo, "app", recent, recent[:12])
	if rc, _, errs := runJob(t, JobSpec{Machine: m, Repo: "app", SHA: recent, Lane: "x", Slot: "h1", Argv: []string{"true"}}); rc != 0 {
		t.Fatalf("rc=%d\n%s", rc, errs)
	}
	refs := gitRun(t, filepath.Join(m.WorkDir, "git", "app.git"), "for-each-ref", "--format=%(refname)", "refs/heavyq/")
	if strings.Contains(refs, old[:12]) || !strings.Contains(refs, recent[:12]) || strings.Contains(refs, "jobs/") {
		t.Fatalf("refs after the job:\n%s", refs)
	}
}
