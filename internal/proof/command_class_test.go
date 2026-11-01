package proof

import (
	"strings"
	"testing"
	"time"

	"github.com/LysCongThanh/heavyq/internal/history"
	"github.com/LysCongThanh/heavyq/internal/queue"
)

// run is a finished run of cmd on treeA as heavyq writes it now: with the hash
// of the exact argv, and the redacted, cut display form in cmd.
func run(cmd string, mod func(*history.Record)) history.Record {
	r := rec(cmd, func(r *history.Record) {
		r.Cmd = queue.TruncateCmd(Argv(cmd))
		r.CmdHash = history.CmdHash(Argv(cmd))
	})
	if mod != nil {
		mod(&r)
	}
	return r
}

func exit(code int) func(*history.Record) { return func(r *history.Record) { r.Exit = code } }

// loaded stamps a failed run with a machine that was past its load limit.
func loaded(r *history.Record) {
	r.Load1, r.MaxLoad, r.Cores, r.FreeMB, r.JobMB = 9.5, 6, 8, 9000, 3072
}

// idle stamps a run with a machine well inside its limits.
func idle(r *history.Record) { r.Load1, r.MaxLoad, r.Cores, r.FreeMB, r.JobMB = 0.4, 6, 8, 9000, 3072 }

func all(fs ...func(*history.Record)) func(*history.Record) {
	return func(r *history.Record) {
		for _, f := range fs {
			if f != nil {
				f(r)
			}
		}
	}
}

func TestClassifyAFailedCommandOnATree(t *testing.T) {
	cases := []struct {
		name       string
		runs       []history.Record
		state      State
		class      Class
		passes     int
		fails      int
		reasonHas  string
		decidingEx int // exit code of the run the status reports
	}{
		{"single pass", []history.Record{run("go test", nil)}, Proven, "", 1, 0, "", 0},
		{"plain failure", []history.Record{run("go test", all(exit(1), idle))}, Failed, ClassFailed, 0, 1, "", 1},
		{"failure without a machine reading", []history.Record{run("go test", exit(1))}, Failed, ClassFailed, 0, 1, "", 1},
		{"pass then fail is flaky", []history.Record{run("go test", nil), run("go test", all(exit(1), idle))},
			Failed, ClassFlaky, 1, 1, "flaky: 1 pass / 1 fail", 1},
		{"fail then pass is flaky", []history.Record{run("go test", all(exit(2), idle)), run("go test", idle)},
			Failed, ClassFlaky, 1, 1, "flaky: 1 pass / 1 fail", 2},
		{"counts every run", []history.Record{run("go test", nil), run("go test", exit(1)), run("go test", exit(1)), run("go test", nil)},
			Failed, ClassFlaky, 2, 2, "flaky: 2 pass / 2 fail", 1},
		{"forced failure is under load", []history.Record{run("go test", all(exit(1), func(r *history.Record) { r.Forced = true }))},
			Failed, ClassUnderLoad, 0, 1, "anti-starvation", 1},
		{"load at the limit is under load", []history.Record{run("go test", all(exit(1), func(r *history.Record) {
			r.Load1, r.MaxLoad, r.Cores, r.FreeMB, r.JobMB = 6, 6, 8, 9000, 3072
		}))}, Failed, ClassUnderLoad, 0, 1, "load 6.00 >= 6.00", 1},
		{"load just below the limit is a plain failure", []history.Record{run("go test", all(exit(1), func(r *history.Record) {
			r.Load1, r.MaxLoad, r.Cores, r.FreeMB, r.JobMB = 5.99, 6, 8, 9000, 3072
		}))}, Failed, ClassFailed, 0, 1, "", 1},
		{"little free RAM is under load", []history.Record{run("go test", all(exit(137), func(r *history.Record) {
			r.Load1, r.MaxLoad, r.Cores, r.FreeMB, r.JobMB = 0.1, 6, 8, 1500, 3072
		}))}, Failed, ClassUnderLoad, 0, 1, "1500 MB free < 3072 MB job budget", 137},
		{"zero free RAM is a reading, not an absence", []history.Record{run("go test", all(exit(1), func(r *history.Record) {
			r.Load1, r.MaxLoad, r.Cores, r.FreeMB, r.JobMB = 0.1, 6, 8, 0, 3072
		}))}, Failed, ClassUnderLoad, 0, 1, "0 MB free", 1},
		{"no reading, no claim: a RAM limit alone is not enough", []history.Record{run("go test", all(exit(1), func(r *history.Record) {
			r.JobMB, r.MaxLoad = 3072, 6 // cores missing: the machine was never measured
		}))}, Failed, ClassFailed, 0, 1, "", 1},
		{"a pass, then a failure under load: under load, not flaky", []history.Record{run("go test", idle), run("go test", all(exit(1), loaded))},
			Failed, ClassUnderLoad, 1, 1, "under load", 1},
		{"a failure under load, then a pass: proven", []history.Record{run("go test", all(exit(1), loaded)), run("go test", idle)},
			Proven, "", 1, 1, "", 0},
		{"a plain failure, a pass and a failure under load: flaky", []history.Record{
			run("go test", all(exit(1), idle)), run("go test", idle), run("go test", all(exit(1), loaded))},
			Failed, ClassFlaky, 1, 2, "flaky: 1 pass / 2 fail", 1},
		{"a plain failure, then one under load: under load", []history.Record{
			run("go test", all(exit(1), idle)), run("go test", all(exit(1), loaded))},
			Failed, ClassUnderLoad, 0, 2, "under load", 1},
		{"repeated cache hits of a pass are not more passes", []history.Record{
			run("go test", exit(1)), run("go test", nil),
			run("go test", func(r *history.Record) { r.Cache = "hit" }), run("go test", func(r *history.Record) { r.Cache = "hit" }),
			run("go test", func(r *history.Record) { r.Cache = "coalesced" })},
			Failed, ClassFlaky, 1, 1, "flaky: 1 pass / 1 fail", 1},
		{"a replayed failure does not lose the load of the run it copies", []history.Record{
			run("go test", all(exit(1), loaded)), run("go test", all(exit(1), func(r *history.Record) { r.Cache = "hit" })),
			run("go test", idle)},
			Proven, "", 1, 1, "", 0},
		{"a result that only arrived by replay counts once", []history.Record{
			run("go test", func(r *history.Record) { r.Cache = "hit" }), run("go test", all(exit(1), func(r *history.Record) { r.Cache = "hit" })),
			run("go test", func(r *history.Record) { r.Cache = "hit" })},
			Failed, ClassFlaky, 1, 1, "flaky: 1 pass / 1 fail", 1},
	}
	subj := Subject{Repo: repoA, Tree: treeA}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			g := Evaluate("ready", []string{"go test"}, subj, c.runs)
			got := g.Commands[0]
			if got.State != c.state || got.Class != c.class || got.Passes != c.passes || got.Fails != c.fails ||
				!strings.Contains(got.Reason, c.reasonHas) || got.Exit == nil || *got.Exit != c.decidingEx {
				t.Fatalf("got %+v (exit %v)", got, got.Exit)
			}
			if g.Proven != (c.state == Proven) {
				t.Fatalf("proven = %v for state %s", g.Proven, c.state)
			}
		})
	}
}

// A flaky command points at the failure that makes it so, not at a later pass.
func TestFlakyReportsTheLatestUnexplainedFailure(t *testing.T) {
	at := func(m int, mod func(*history.Record)) func(*history.Record) {
		return all(func(r *history.Record) { r.End = base.Add(time.Duration(m) * time.Minute) }, mod)
	}
	runs := []history.Record{
		run("go test", at(1, exit(1))), run("go test", at(2, exit(7))),
		run("go test", at(3, all(exit(1), loaded))), run("go test", at(4, nil)),
	}
	c := Evaluate("ready", []string{"go test"}, Subject{Repo: repoA, Tree: treeA}, runs).Commands[0]
	if c.Class != ClassFlaky || *c.Exit != 7 || !c.At.Equal(base.Add(2*time.Minute)) {
		t.Fatalf("%+v", c)
	}
}

func TestClassIsOnlyForFailedCommands(t *testing.T) {
	subj := Subject{Repo: repoA, Tree: treeA}
	g := Evaluate("ready", []string{"ok", "never"}, subj, []history.Record{run("ok", nil)})
	for _, c := range g.Commands {
		if c.Class != "" {
			t.Fatalf("%s is %s: class must be empty, got %q", c.Cmd, c.State, c.Class)
		}
	}
}

func TestOtherTreesDoNotMakeACommandFlaky(t *testing.T) {
	runs := []history.Record{run("go test", func(r *history.Record) { r.Tree = treeB; r.Exit = 1 }), run("go test", nil)}
	c := Evaluate("ready", []string{"go test"}, Subject{Repo: repoA, Tree: treeA}, runs).Commands[0]
	if c.State != Proven || c.Class != "" || c.Fails != 0 {
		t.Fatalf("%+v", c)
	}
}
