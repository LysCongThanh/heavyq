package snapshot

import (
	"strings"
	"testing"
	"time"

	"github.com/LysCongThanh/heavyq/internal/history"
)

var exAt = time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)

const (
	exTree = "4f2a1c0000000000000000000000000000000000"
	exCmd  = "go test ./billing/..."
)

func failRec(lane string, ago time.Duration, mod func(*history.Record)) history.Record {
	r := history.Record{V: 2, End: exAt.Add(-ago), Lane: lane, Machine: "local", Exit: 1, Cmd: "sh -c " + exCmd, Repo: "r", Tree: exTree}
	if mod != nil {
		mod(&r)
	}
	return r
}

func kinds(es []Exception) []string {
	out := make([]string, len(es))
	for i, e := range es {
		out[i] = e.Kind + ":" + e.Lane + e.Machine
	}
	return out
}

func gateOf(state string, failed ...GateFailure) LaneGate {
	return LaneGate{Lane: "wt-billing", Gate: "ready", Tree: exTree, State: state, Failed: failed, Worktree: "/work/it's"}
}

func TestComputeExceptionsPerKind(t *testing.T) {
	failure := GateFailure{Cmd: exCmd, At: exAt.Add(-time.Minute), Count: 2}
	blocked := map[string]string{"local": "all heavy slots busy"}
	waiting := func(wait float64, prio int, reasons map[string]string) Waiting {
		return Waiting{Ticket: Ticket{ID: "t-head", Lane: "wt-search", Prio: prio, WaitS: wait, Cmd: "npm test"}, Position: 1, Reasons: reasons}
	}
	cfg := Config{ForceAfterS: 900}
	cases := []struct {
		name  string
		snap  Snapshot
		recs  []history.Record
		want  []string
		check func(t *testing.T, es []Exception)
	}{
		{name: "nothing", want: []string{}},
		{name: "proven and unproven gates are no exception",
			snap: Snapshot{Gates: []LaneGate{gateOf(GateProven), gateOf(GateUnproven), gateOf(GateSkipped)}}, want: []string{}},
		{name: "red gate", snap: Snapshot{Gates: []LaneGate{gateOf(GateFailed, failure)}}, want: []string{"red_gate:wt-billing"},
			check: func(t *testing.T, es []Exception) {
				e := es[0]
				if e.Severity != SeverityCritical || e.Evidence.Tree != exTree || e.Evidence.Count != 2 || e.Evidence.Gate != "ready" ||
					e.Action == nil || e.Action.Kind != ActionRun || e.Action.Command != `cd '/work/it'\''s' && heavyq gate run ready` ||
					!strings.Contains(e.Text, "`"+exCmd+"`") || !strings.HasSuffix(e.Text, "failed.") {
					t.Fatalf("%+v action=%+v", e, e.Action)
				}
			}},
		{name: "flaky and under-load failures are noise",
			snap: Snapshot{Gates: []LaneGate{gateOf(GateFailed, GateFailure{Cmd: "a", Class: ClassFlaky}, GateFailure{Cmd: "b", Class: ClassUnderLoad})}},
			want: []string{}},
		{name: "one real failure among noise still counts",
			snap: Snapshot{Gates: []LaneGate{gateOf(GateFailed, GateFailure{Cmd: "a", Class: ClassFlaky}, failure)}}, want: []string{"red_gate:wt-billing"}},
		{name: "loop: twice on the same tree within 30 minutes",
			snap: Snapshot{Running: []Running{{Ticket: Ticket{ID: "t-run", Lane: "wt-billing"}}}},
			recs: []history.Record{failRec("wt-billing", 20*time.Minute, nil), failRec("wt-billing", 2*time.Minute, nil)},
			want: []string{"loop:wt-billing"},
			check: func(t *testing.T, es []Exception) {
				e := es[0]
				if e.Evidence.Count != 2 || e.Evidence.Tree != exTree || !e.Evidence.At.Equal(exAt.Add(-2*time.Minute)) ||
					e.Action == nil || e.Action.Kind != ActionCancel || e.Action.Target != "t-run" || e.Action.Command != "heavyq cancel t-run" {
					t.Fatalf("%+v action=%+v", e, e.Action)
				}
			}},
		{name: "loop without a job to stop has no action",
			recs: []history.Record{failRec("a", 5*time.Minute, nil), failRec("a", time.Minute, nil), failRec("a", 30*time.Second, nil)},
			want: []string{"loop:a"}, check: func(t *testing.T, es []Exception) {
				if es[0].Action != nil || es[0].Evidence.Count != 3 {
					t.Fatalf("%+v", es[0])
				}
			}},
		{name: "once is not a loop", recs: []history.Record{failRec("a", time.Minute, nil)}, want: []string{}},
		{name: "two failures, one older than 30 minutes", recs: []history.Record{failRec("a", 31*time.Minute, nil), failRec("a", time.Minute, nil)}, want: []string{}},
		{name: "failures on different trees, commands or lanes are not a loop", recs: []history.Record{
			failRec("a", 3*time.Minute, nil), failRec("a", 2*time.Minute, func(r *history.Record) { r.Tree = "other" }),
			failRec("a", time.Minute, func(r *history.Record) { r.Cmd = "sh -c other" }), failRec("b", time.Minute, nil)}, want: []string{}},
		{name: "the same command under another run_id is not a retry", recs: []history.Record{
			failRec("a", 3*time.Minute, func(r *history.Record) { r.RunID = "id-with-env" }),
			failRec("a", 2*time.Minute, func(r *history.Record) { r.RunID = "id-without" })}, want: []string{}},
		{name: "the same run_id is a loop even when the display form differs", recs: []history.Record{
			failRec("a", 3*time.Minute, func(r *history.Record) { r.RunID = "id1" }),
			failRec("a", 2*time.Minute, func(r *history.Record) { r.RunID, r.Cmd = "id1", "sh -c cut..." })}, want: []string{"loop:a"}},
		{name: "cache hits, cancelled runs and passes are not failures", recs: []history.Record{
			failRec("a", 4*time.Minute, func(r *history.Record) { r.Cache = "hit" }),
			failRec("a", 3*time.Minute, func(r *history.Record) { r.Result = history.OutcomeCancelled }),
			failRec("a", 2*time.Minute, func(r *history.Record) { r.Exit = 0 }),
			failRec("a", time.Minute, nil), failRec("a", time.Minute, func(r *history.Record) { r.Tree = "" })}, want: []string{}},
		{name: "starved: head waited past two thirds of force_after",
			snap: Snapshot{Config: cfg, Waiting: []Waiting{waiting(601, 3, blocked)}}, want: []string{"starved:wt-search"},
			check: func(t *testing.T, es []Exception) {
				a := es[0].Action
				if es[0].Severity != SeverityWarning || a == nil || a.Kind != ActionRaise || a.Target != "t-head" || a.Value != 2 || a.Command != "heavyq reprio t-head 2" ||
					!strings.Contains(es[0].Text, "10m 01s") {
					t.Fatalf("%+v action=%+v", es[0], a)
				}
			}},
		{name: "starved: not yet at two thirds", snap: Snapshot{Config: cfg, Waiting: []Waiting{waiting(600, 3, blocked)}}, want: []string{}},
		{name: "starved: a head that starts on the next poll is fine",
			snap: Snapshot{Config: cfg, Waiting: []Waiting{waiting(800, 3, map[string]string{"local": ""})}}, want: []string{}},
		{name: "starved: no force_after, no bypass to wait for", snap: Snapshot{Waiting: []Waiting{waiting(5000, 3, blocked)}}, want: []string{}},
		{name: "starved: highest priority has nothing to raise",
			snap: Snapshot{Config: cfg, Waiting: []Waiting{waiting(700, 1, blocked)}}, want: []string{"starved:wt-search"},
			check: func(t *testing.T, es []Exception) {
				if es[0].Action != nil {
					t.Fatalf("%+v", es[0].Action)
				}
			}},
		{name: "starved: a give-up in the last hour", recs: []history.Record{
			failRec("wt-docs", 59*time.Minute, func(r *history.Record) { r.Result = history.OutcomeGaveUp }),
			failRec("wt-old", 61*time.Minute, func(r *history.Record) { r.Result = history.OutcomeGaveUp })},
			want: []string{"starved:wt-docs"}},
		{name: "starved: head and give-ups of one lane make one card",
			snap: Snapshot{Config: cfg, Waiting: []Waiting{waiting(700, 3, blocked)}},
			recs: []history.Record{failRec("wt-search", time.Minute, func(r *history.Record) { r.Result = history.OutcomeGaveUp }),
				failRec("wt-search", 2*time.Minute, func(r *history.Record) { r.Result = history.OutcomeGaveUp })},
			want: []string{"starved:wt-search"}, check: func(t *testing.T, es []Exception) {
				if es[0].Evidence.Count != 2 || !strings.Contains(es[0].Text, "gave up 2 times") || !strings.Contains(es[0].Text, "head of the queue") {
					t.Fatalf("%+v", es[0])
				}
			}},
		{name: "machine: unreachable and paused",
			snap: Snapshot{Machines: []Machine{{Name: "local", Reachable: true}, {Name: "old-laptop"}, {Name: "box", Reachable: true, Paused: true}}},
			want: []string{"machine:box", "machine:old-laptop"}, check: func(t *testing.T, es []Exception) {
				if a := es[0].Action; a == nil || a.Kind != ActionResume || a.Target != "box" || a.Command != "heavyq resume box" || es[1].Action != nil {
					t.Fatalf("%+v %+v", es[0], es[1])
				}
			}},
		{name: "machine: outside RAM above 40%",
			snap: Snapshot{Machines: []Machine{{Name: "local", Reachable: true, TotalMB: 30000}},
				CPU: CPU{Available: true, Outside: []Usage{{Name: "claude", Procs: 14, RSSMB: 11000}}, OutsideTotal: Usage{RSSMB: 12001}}},
			want: []string{"machine:local"}, check: func(t *testing.T, es []Exception) {
				if !strings.Contains(es[0].Text, "led by claude (14 processes)") || es[0].Evidence.Detail == "" {
					t.Fatalf("%+v", es[0])
				}
			}},
		{name: "machine: outside RAM at 40% or unmeasured is fine",
			snap: Snapshot{Machines: []Machine{{Name: "local", Reachable: true, TotalMB: 30000}}, CPU: CPU{Available: true, OutsideTotal: Usage{RSSMB: 12000}}}, want: []string{}},
		{name: "machine: no /proc, no verdict",
			snap: Snapshot{Machines: []Machine{{Name: "local", Reachable: true, TotalMB: 30000}}, CPU: CPU{OutsideTotal: Usage{RSSMB: 29000}}}, want: []string{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			es := ComputeExceptions(c.snap, c.recs, exAt)
			if es == nil {
				t.Fatal("must be an empty list, not nil: JSON consumers expect []")
			}
			got := kinds(es)
			if strings.Join(got, ",") != strings.Join(c.want, ",") {
				t.Fatalf("exceptions = %v, want %v", got, c.want)
			}
			if c.check != nil {
				c.check(t, es)
			}
		})
	}
}

func TestExceptionsOrderAndDedup(t *testing.T) {
	s := Snapshot{
		Machines: []Machine{{Name: "box", Reachable: true, Paused: true}},
		Gates:    []LaneGate{gateOf(GateFailed, GateFailure{Cmd: exCmd, At: exAt}), gateOf(GateFailed, GateFailure{Cmd: exCmd, At: exAt})},
	}
	recs := []history.Record{failRec("a", 2*time.Minute, nil), failRec("a", time.Minute, nil),
		failRec("z", time.Minute, func(r *history.Record) { r.Result = history.OutcomeGaveUp })}
	got := kinds(ComputeExceptions(s, recs, exAt))
	want := []string{"red_gate:wt-billing", "loop:a", "starved:z", "machine:box"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("got %v, want %v (critical first, one card per identical exception)", got, want)
	}
}

func TestExceptionIDChangesWithEvidence(t *testing.T) {
	two := ComputeExceptions(Snapshot{}, []history.Record{failRec("a", 2*time.Minute, nil), failRec("a", time.Minute, nil)}, exAt)
	three := ComputeExceptions(Snapshot{}, []history.Record{failRec("a", 3*time.Minute, nil), failRec("a", 2*time.Minute, nil), failRec("a", time.Minute, nil)}, exAt)
	if len(two) != 1 || len(three) != 1 || two[0].ID == three[0].ID {
		t.Fatalf("a dismissed loop must come back when it gets worse: %q vs %q", two[0].ID, three[0].ID)
	}
}

func TestShellQuote(t *testing.T) {
	for in, want := range map[string]string{"/a/b-c_d.e": "/a/b-c_d.e", "": "''", "a b": "'a b'", "it's": `'it'\''s'`, "$(x)": "'$(x)'"} {
		if got := shellQuote(in); got != want {
			t.Errorf("shellQuote(%q) = %s, want %s", in, got, want)
		}
	}
}
