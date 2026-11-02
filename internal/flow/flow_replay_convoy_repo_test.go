package flow_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

// convoyRepo turns the plan into a real git repository: branches, commits at
// the planned times, and a main that moves under every branch so that each
// merge of main conflicts in the three hot files. Nothing about the history is
// faked: the git collector reads exactly what git recorded.
type convoyRepo struct {
	t   *testing.T
	dir string
}

type gitStep struct {
	at time.Time
	do func()
}

// git runs git in the repository at a given time and fails the test on a
// non-zero exit.
func (r *convoyRepo) git(at time.Time, args ...string) {
	r.t.Helper()
	if out, err := r.gitMayFail(at, args...); err != nil {
		r.t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// gitMayFail is git for the one call allowed to fail: a merge that conflicts
// exits 1.
func (r *convoyRepo) gitMayFail(at time.Time, args ...string) ([]byte, error) {
	cmd := exec.Command("git", append([]string{"-C", r.dir, "-c", "commit.gpgsign=false", "-c", "core.hooksPath=/dev/null"}, args...)...)
	stamp := at.UTC().Format(time.RFC3339)
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_COMMITTER_NAME=t",
		"GIT_COMMITTER_EMAIL=t@example.com", "GIT_AUTHOR_DATE="+stamp, "GIT_COMMITTER_DATE="+stamp)
	return cmd.CombinedOutput()
}

func (r *convoyRepo) write(files map[string]string) {
	r.t.Helper()
	for name, body := range files {
		p := filepath.Join(r.dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			r.t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			r.t.Fatal(err)
		}
	}
}

// hot is the three hot files with one slot line that every change rewrites.
func hot(slot string) map[string]string {
	m := map[string]string{}
	for _, f := range hotFiles {
		m[f] = "# " + f + "\n" + slot + "\n# end\n"
	}
	return m
}

func (r *convoyRepo) commit(at time.Time, msg string, files map[string]string) {
	r.t.Helper()
	r.write(files)
	r.git(at, "add", "-A")
	r.git(at, "commit", "-q", "-m", msg)
}

// buildConvoyRepo creates the repository and replays the plan into it, in the
// order of time.
func buildConvoyRepo(t *testing.T, c convoy) string {
	t.Helper()
	r := &convoyRepo{t: t, dir: filepath.Join(t.TempDir(), "app")}
	if err := os.MkdirAll(r.dir, 0o755); err != nil {
		t.Fatal(err)
	}
	r.git(minute(-60), "init", "-q", "-b", "main")
	r.commit(minute(-60), "initial", hot("base"))

	var steps []gitStep
	for _, p := range c.PRs {
		steps = append(steps,
			gitStep{p.Start, func() {
				r.git(p.Start, "checkout", "-q", "-b", p.Branch(), "main")
				r.commit(p.Commits[0], "feat: "+p.ID, mergeFiles(hot("pr-"+p.ID), map[string]string{"internal/" + p.ID + "/impl.go": "package x\n"}))
			}},
			gitStep{p.Commits[1], func() {
				r.git(p.Commits[1], "checkout", "-q", p.Branch())
				r.commit(p.Commits[1], "wip: "+p.ID, map[string]string{"internal/" + p.ID + "/impl.go": "package x // wip\n"})
			}})
		for _, at := range p.Merges {
			steps = append(steps, gitStep{at, func() { r.mergeMain(p, at) }})
		}
	}
	for _, m := range c.MainCommits {
		if strings.HasPrefix(m.Label, "landed-") { // a squash merge: the pull request's changes in one commit on main
			steps = append(steps, gitStep{m.At, func() {
				r.git(m.At, "checkout", "-q", "main")
				r.commit(m.At, m.Label+" (#1)", hot(m.Label))
			}})
			continue
		}
		steps = append(steps, gitStep{m.At, func() {
			r.git(m.At, "checkout", "-q", "main")
			r.commit(m.At, "chore: "+m.Label, hot(m.Label))
		}})
	}
	sort.SliceStable(steps, func(i, j int) bool { return steps[i].at.Before(steps[j].at) })
	for _, s := range steps {
		s.do()
	}
	return r.dir
}

// mergeMain merges main into the pull request's branch and resolves the
// conflicts by keeping both sides, as a person would.
func (r *convoyRepo) mergeMain(p *convoyPR, at time.Time) {
	r.t.Helper()
	r.git(at, "checkout", "-q", p.Branch())
	if _, err := r.gitMayFail(at, "merge", "--no-edit", "--no-ff", "main"); err == nil {
		r.t.Fatalf("%s: the merge of main at %v must conflict in the hot files (a plan error)", p.ID, at)
	}
	r.write(hot(fmt.Sprintf("pr-%s+main@%s", p.ID, at.Format("15:04"))))
	r.git(at, "add", "-A")
	r.git(at, "commit", "-q", "--no-edit")
}

func mergeFiles(a, b map[string]string) map[string]string {
	for k, v := range b {
		a[k] = v
	}
	return a
}
