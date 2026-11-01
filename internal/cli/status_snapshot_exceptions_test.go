package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LysCongThanh/heavyq/internal/config"
	"github.com/LysCongThanh/heavyq/internal/gitutil"
	"github.com/LysCongThanh/heavyq/internal/history"
	"github.com/LysCongThanh/heavyq/internal/proof"
	"github.com/LysCongThanh/heavyq/internal/queue"
	"github.com/LysCongThanh/heavyq/internal/snapshot"
)

func TestFailureClassMapsTheProofClasses(t *testing.T) {
	cases := map[proof.Class]string{
		proof.ClassFlaky: snapshot.ClassFlaky, proof.ClassUnderLoad: snapshot.ClassUnderLoad, proof.ClassFailed: "", "": "",
	}
	for in, want := range cases {
		if got := failureClass(proof.Command{State: proof.Failed, Class: in}); got != want {
			t.Errorf("class %q -> %q, want %q", in, got, want)
		}
	}
}

// Real worktree, real heavyq.toml, ledger records written by hand: only a
// plain failure of the gate raises red_gate; a flaky or under-load failure is
// noise, and a command that was proven again by repetition is not a failure.
func TestFlakyAndUnderLoadFailuresNeverRaiseRedGate(t *testing.T) {
	long := "go test " + strings.Repeat("-run X ", 40) // over 200 bytes: matched by hash
	repo := t.TempDir()
	for _, args := range [][]string{{"init", "-q", "-b", "main"}, {"config", "commit.gpgsign", "false"}} {
		mustGit(t, repo, args...)
	}
	toml := "[[gate]]\nname = \"ready\"\nrun = ['" + long + "']\n"
	if err := os.WriteFile(filepath.Join(repo, "heavyq.toml"), []byte(toml), 0o644); err != nil {
		t.Fatal(err)
	}
	mustGit(t, repo, "add", ".")
	mustGit(t, repo, "commit", "-q", "-m", "gate")
	co, err := gitutil.Identify(repo)
	if err != nil || co.Tree == "" {
		t.Fatalf("identify: %+v %v", co, err)
	}
	run := func(exit int, mod func(*history.Record)) history.Record {
		r := history.Record{V: 2, End: time.Now(), Lane: "wt", Machine: "local", Exit: exit, Repo: co.Repo, Tree: co.Tree,
			Cmd: queue.TruncateCmd(proof.Argv(long)), CmdHash: history.CmdHash(proof.Argv(long))}
		if mod != nil {
			mod(&r)
		}
		return r
	}
	forced := func(r *history.Record) { r.Forced = true }
	cases := []struct {
		name    string
		recs    []history.Record
		state   string
		class   string
		wantRed bool
	}{
		{"plain failure", []history.Record{run(1, nil)}, snapshot.GateFailed, "", true},
		{"flaky: pass then fail", []history.Record{run(0, nil), run(1, nil)}, snapshot.GateFailed, snapshot.ClassFlaky, false},
		{"flaky: fail then one pass", []history.Record{run(1, nil), run(0, nil)}, snapshot.GateFailed, snapshot.ClassFlaky, false},
		{"under load: forced failure", []history.Record{run(1, forced)}, snapshot.GateFailed, snapshot.ClassUnderLoad, false},
		{"proven again by repetition", []history.Record{run(1, nil), run(0, nil), run(0, nil)}, snapshot.GateProven, "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			g := worktreeGate(&config.Config{}, repo, c.recs)
			if g.State != c.state {
				t.Fatalf("state = %s, want %s (%+v)", g.State, c.state, g)
			}
			if c.class != "" && (len(g.Failed) != 1 || g.Failed[0].Class != c.class) {
				t.Fatalf("failures = %+v, want class %q", g.Failed, c.class)
			}
			g.Lane = "wt"
			ex := snapshot.ComputeExceptions(snapshot.Snapshot{Gates: []snapshot.LaneGate{g}}, c.recs, time.Now())
			red := false
			for _, e := range ex {
				red = red || e.Kind == snapshot.KindRedGate
			}
			if red != c.wantRed {
				t.Fatalf("red_gate = %v, want %v: %+v", red, c.wantRed, ex)
			}
		})
	}
}

func mustGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}
