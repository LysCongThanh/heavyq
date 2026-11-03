package proof

import (
	"strings"
	"testing"
	"time"

	"github.com/LysCongThanh/heavyq/internal/history"
	"github.com/LysCongThanh/heavyq/internal/queue"
)

const (
	repoA = "aaaaaaaaaaaaaaaa"
	treeA = "1111111111111111111111111111111111111111"
	treeB = "2222222222222222222222222222222222222222"
)

var base = time.Date(2026, 11, 2, 10, 0, 0, 0, time.UTC)

// rec is a finished run of the gate command cmd on treeA in repoA.
func rec(cmd string, mod func(*history.Record)) history.Record {
	r := history.Record{V: 2, End: base, Lane: "lane", Machine: "local", RunSecs: 2, Exit: 0,
		Cmd: queue.TruncateCmd(Argv(cmd)), Repo: repoA, Tree: treeA, Commit: "c0"}
	if mod != nil {
		mod(&r)
	}
	return r
}

func states(g Gate) []State {
	var out []State
	for _, c := range g.Commands {
		out = append(out, c.State)
	}
	return out
}

func TestEvaluateStates(t *testing.T) {
	subj := Subject{EnvFree: true, Repo: repoA, Tree: treeA}
	cmds := []string{"fmt", "vet", "test"}
	at := func(m int) func(*history.Record) {
		return func(r *history.Record) { r.End = base.Add(time.Duration(m) * time.Minute) }
	}
	cases := []struct {
		name      string
		recs      []history.Record
		want      []State
		wantProof bool
	}{
		{"no records", nil, []State{Missing, Missing, Missing}, false},
		{"all passed", []history.Record{rec("fmt", nil), rec("vet", nil), rec("test", nil)},
			[]State{Proven, Proven, Proven}, true},
		{"one missing", []history.Record{rec("fmt", nil), rec("test", nil)},
			[]State{Proven, Missing, Proven}, false},
		{"one failed", []history.Record{rec("fmt", nil), rec("vet", func(r *history.Record) { r.Exit = 1 }), rec("test", nil)},
			[]State{Proven, Failed, Proven}, false},
		{"latest wins: fail after pass", []history.Record{rec("fmt", at(1)), rec("fmt", func(r *history.Record) { r.Exit = 3 }), rec("vet", nil), rec("test", nil)},
			[]State{Failed, Proven, Proven}, false},
		{"a single pass after a failure is flaky, not proven yet", []history.Record{rec("fmt", func(r *history.Record) { r.Exit = 3 }), rec("fmt", nil), rec("vet", nil), rec("test", nil)},
			[]State{Failed, Proven, Proven}, false},
		{"two passes after a failure prove it", []history.Record{rec("fmt", func(r *history.Record) { r.Exit = 3 }), rec("fmt", nil), rec("fmt", nil), rec("vet", nil), rec("test", nil)},
			[]State{Proven, Proven, Proven}, true},
		{"a pass after a failure under load is proven", []history.Record{
			rec("fmt", func(r *history.Record) { r.Exit, r.Forced = 3, true }), rec("fmt", nil), rec("vet", nil), rec("test", nil)},
			[]State{Proven, Proven, Proven}, true},
		{"cache hit and coalesced count", []history.Record{
			rec("fmt", func(r *history.Record) { r.Cache = "hit" }),
			rec("vet", func(r *history.Record) { r.Cache = "coalesced" }), rec("test", nil)},
			[]State{Proven, Proven, Proven}, true},
		{"another tree", []history.Record{rec("fmt", func(r *history.Record) { r.Tree = treeB }), rec("vet", nil), rec("test", nil)},
			[]State{Missing, Proven, Proven}, false},
		{"another repo", []history.Record{rec("fmt", func(r *history.Record) { r.Repo = "bbbbbbbbbbbbbbbb" }), rec("vet", nil), rec("test", nil)},
			[]State{Missing, Proven, Proven}, false},
		{"another worktree of the same repo is proof", []history.Record{
			rec("fmt", func(r *history.Record) { r.Worktree = "/elsewhere" }), rec("vet", nil), rec("test", nil)},
			[]State{Proven, Proven, Proven}, true},
		{"run from a subdirectory is another run", []history.Record{
			rec("fmt", func(r *history.Record) { r.Subdir = "pkg" }), rec("vet", nil), rec("test", nil)},
			[]State{Missing, Proven, Proven}, false},
		{"other command string", []history.Record{rec("fmt ./...", nil), rec("vet", nil), rec("test", nil)},
			[]State{Missing, Proven, Proven}, false},
		{"run without tree identity", []history.Record{rec("fmt", func(r *history.Record) { r.Tree = "" }), rec("vet", nil), rec("test", nil)},
			[]State{Missing, Proven, Proven}, false},
		{"gave up, cancelled, interrupted are inconclusive", []history.Record{
			rec("fmt", func(r *history.Record) { r.Result = history.OutcomeGaveUp }),
			rec("vet", func(r *history.Record) { r.Result = history.OutcomeCancelled }),
			rec("test", func(r *history.Record) { r.Result = history.OutcomeInterrupted })},
			[]State{Missing, Missing, Missing}, false},
		{"inconclusive run does not undo a proof", []history.Record{rec("fmt", nil),
			rec("fmt", func(r *history.Record) { r.Result = history.OutcomeInterrupted; r.Exit = 130 }),
			rec("vet", nil), rec("test", nil)},
			[]State{Proven, Proven, Proven}, true},
		{"interrupted does not hide a failure either", []history.Record{
			rec("fmt", func(r *history.Record) { r.Exit = 2 }),
			rec("fmt", func(r *history.Record) { r.Result = history.OutcomeGaveUp }),
			rec("vet", nil), rec("test", nil)},
			[]State{Failed, Proven, Proven}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			g := Evaluate("ready", cmds, subj, c.recs)
			got := states(g)
			for i := range c.want {
				if got[i] != c.want[i] {
					t.Fatalf("states = %v, want %v", got, c.want)
				}
			}
			if g.Proven != c.wantProof {
				t.Fatalf("proven = %v, want %v (%v)", g.Proven, c.wantProof, got)
			}
		})
	}
}

func TestEvaluateReportsTheDecidingRun(t *testing.T) {
	recs := []history.Record{rec("go test", func(r *history.Record) {
		r.Lane, r.RunSecs, r.Cache, r.Machine, r.End = "agent-3", 41.5, "hit", "box", base.Add(time.Hour)
	})}
	g := Evaluate("ready", []string{"go test"}, Subject{EnvFree: true, Repo: repoA, Tree: treeA, Commit: "c0", Dirty: true}, recs)
	c := g.Commands[0]
	if !g.Proven || c.Lane != "agent-3" || c.RunSecs != 41.5 || c.Cache != "hit" || c.Machine != "box" ||
		!c.At.Equal(base.Add(time.Hour)) || c.Exit == nil || *c.Exit != 0 || !g.Dirty || g.Commit != "c0" {
		t.Fatalf("%+v", g)
	}
}

func TestUnknownTreeProvesNothingAndSaysWhy(t *testing.T) {
	recs := []history.Record{rec("fmt", nil), rec("fmt", func(r *history.Record) { r.Tree = "" })}
	g := Evaluate("ready", []string{"fmt"}, Subject{EnvFree: true, Repo: repoA, Dirty: true, Why: "a submodule has changes of its own"}, recs)
	if g.Proven || g.Commands[0].State != Missing || !strings.Contains(g.Reason, "a submodule has changes of its own") ||
		g.Commands[0].Reason == "" {
		t.Fatalf("%+v", g)
	}
	// Even a record with an empty tree must not match an empty subject tree.
	g = Evaluate("ready", []string{"fmt"}, Subject{}, []history.Record{rec("fmt", func(r *history.Record) { r.Tree, r.Repo = "", "" })})
	if g.Proven || !strings.Contains(g.Reason, "could not be identified") {
		t.Fatalf("%+v", g)
	}
}

func TestEmptyGateIsNotProven(t *testing.T) {
	if g := Evaluate("ready", nil, Subject{EnvFree: true, Repo: repoA, Tree: treeA}, nil); g.Proven {
		t.Fatal("a gate without commands proves nothing")
	}
}

func TestLongCommandsWithoutAHashAreNeverMatched(t *testing.T) {
	long := "echo " + strings.Repeat("x", 300)
	w := wantCommand(long)
	if w.textOK {
		t.Fatalf("a %d-byte command must be unmatchable by text, ledger form %q", len(long), w.text)
	}
	r := rec("", func(r *history.Record) { r.Cmd = w.text })
	g := Evaluate("ready", []string{long}, Subject{EnvFree: true, Repo: repoA, Tree: treeA}, []history.Record{r})
	if g.Proven || g.Commands[0].State != Missing || !strings.Contains(g.Commands[0].Reason, "ledger") {
		t.Fatalf("%+v", g)
	}
}

func TestSecretsInACommandStillMatch(t *testing.T) {
	cmd := "API_TOKEN=abc123 make check"
	w := wantCommand(cmd)
	form := w.text
	if !w.textOK || strings.Contains(form, "abc123") {
		t.Fatalf("ledger form %q ok=%v", form, w.textOK)
	}
	g := Evaluate("ready", []string{cmd}, Subject{EnvFree: true, Repo: repoA, Tree: treeA}, []history.Record{rec("", func(r *history.Record) { r.Cmd = form })})
	if !g.Proven {
		t.Fatalf("%+v", g)
	}
}

// The ledger spells a run with the same function Argv feeds; this pins the
// spelling gate run relies on.
func TestArgvIsAShell(t *testing.T) {
	a := Argv("a && b")
	if len(a) != 5 || a[0] != "sh" || a[1] != "-c" || a[3] != GateMarker || a[4] != "a && b" {
		t.Fatal(a)
	}
	if !strings.Contains(a[2], "pipefail") || strings.Count(a[2], `"$1"`) != 2 {
		t.Fatalf("the script must run $1 under pipefail: %s", a[2])
	}
}
