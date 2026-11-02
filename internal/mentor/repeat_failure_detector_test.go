package mentor

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/LysCongThanh/heavyq/internal/history"
)

var t0 = time.Date(2026, 11, 2, 9, 0, 0, 0, time.UTC)

// run builds the n-th ledger record of a lane: run_id and exit are the point,
// the rest is a calm machine with a known tree.
func run(n int, runID string, exit int, mod ...func(*history.Record)) history.Record {
	r := history.Record{V: 2, ID: fmt.Sprintf("id%02d", n), End: t0.Add(time.Duration(n) * 20 * time.Second), Lane: "wt",
		Cmd: "go test ./...", RunID: runID, Tree: "tree-" + runID, Exit: exit,
		Cores: 8, Load1: 0.1, MaxLoad: 6, FreeMB: 20000, JobMB: 1000}
	for _, m := range mod {
		m(&r)
	}
	return r
}

func under(r *history.Record)  { r.Load1 = 9 } // started over the load limit: under_load
func replay(r *history.Record) { r.Cache = "hit" }
func gaveUp(r *history.Record) { r.Result = history.OutcomeGaveUp }
func noTree(r *history.Record) { r.Tree, r.RunID = "", "" }
func forced(r *history.Record) { r.Forced = true }
func withCmd(c string) func(*history.Record) {
	return func(r *history.Record) { r.Cmd = c }
}

func TestDetectRepeatFailure(t *testing.T) {
	cases := []struct {
		name  string
		recs  []history.Record
		level int // 0 = no signal
		count int
	}{
		{"first failure is not a repeat", []history.Record{run(1, "A", 1)}, 0, 0},
		{"2nd failure: a fact", []history.Record{run(1, "A", 1), run(2, "A", 1)}, LevelFact, 2},
		{"3rd failure: a directive", []history.Record{run(1, "A", 2), run(2, "A", 2), run(3, "A", 2)}, LevelDirective, 3},
		{"4th failure stays a directive", []history.Record{run(1, "A", 2), run(2, "A", 2), run(3, "A", 2), run(4, "A", 2)}, LevelDirective, 4},
		{"other commands in between do not break it", []history.Record{run(1, "A", 1), run(2, "B", 0), run(3, "A", 1)}, LevelFact, 2},
		{"a different exit code is not the same failure", []history.Record{run(1, "A", 1), run(2, "A", 2)}, 0, 0},
		{"streak counts only the same exit code", []history.Record{run(1, "A", 2), run(2, "A", 1), run(3, "A", 1)}, LevelFact, 2},
		{"another run_id is another signature", []history.Record{run(1, "A", 1), run(2, "B", 1)}, 0, 0},
		{"flaky: it passed on this tree", []history.Record{run(1, "A", 0), run(2, "A", 1), run(3, "A", 1)}, 0, 0},
		{"flaky: passed in between", []history.Record{run(1, "A", 1), run(2, "A", 0), run(3, "A", 1)}, 0, 0},
		{"under_load: the repeat started over the limit", []history.Record{run(1, "A", 1), run(2, "A", 1, under)}, 0, 0},
		{"under_load: the first failure was loaded", []history.Record{run(1, "A", 1, under), run(2, "A", 1)}, 0, 0},
		{"under_load: anti-starvation bypass", []history.Record{run(1, "A", 1), run(2, "A", 1, forced)}, 0, 0},
		{"timeout 124 never fires", []history.Record{run(1, "A", 124), run(2, "A", 124), run(3, "A", 124)}, 0, 0},
		{"kill 137 never fires", []history.Record{run(1, "A", 137), run(2, "A", 137)}, 0, 0},
		{"heavyq's own 75 never fires", []history.Record{run(1, "A", 75), run(2, "A", 75)}, 0, 0},
		{"SIGTERM 143 never fires", []history.Record{run(1, "A", 143), run(2, "A", 143)}, 0, 0},
		{"a pass is not a failure", []history.Record{run(1, "A", 0), run(2, "A", 0)}, 0, 0},
		{"unknown tree: no run_id, no claim", []history.Record{run(1, "A", 1, noTree), run(2, "A", 1, noTree)}, 0, 0},
		{"cache replays are not new failures", []history.Record{run(1, "A", 1), run(2, "A", 1, replay)}, 0, 0},
		{"a replay in the middle is not counted", []history.Record{run(1, "A", 1), run(2, "A", 1, replay), run(3, "A", 1)}, LevelFact, 2},
		{"a run that gave up is not a failure", []history.Record{run(1, "A", 1), run(2, "A", 1, gaveUp)}, 0, 0},
		{"no records", nil, 0, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			sig, ok := DetectRepeatFailure(c.recs)
			if ok != (c.level != 0) {
				t.Fatalf("signal = %v (%+v), want %v", ok, sig, c.level != 0)
			}
			if ok && (sig.Level != c.level || sig.Streak != c.count || sig.Rule != RuleRepeatFailure) {
				t.Fatalf("got level %d streak %d, want %d/%d", sig.Level, sig.Streak, c.level, c.count)
			}
		})
	}
}

func TestDetectUsesTheNewestRecordAndSanitisesTheCommand(t *testing.T) {
	recs := []history.Record{run(1, "A", 1), run(2, "A", 1, withCmd("go test `id` \n./... "+strings.Repeat("x", 100)))}
	sig, ok := DetectRepeatFailure(recs)
	if !ok || sig.Event != "id02" || sig.Signature != "A" || sig.Tree != "tree-A" || sig.Exit != 1 {
		t.Fatalf("%+v %v", sig, ok)
	}
	if strings.ContainsAny(sig.Cmd, "`\n") || len([]rune(sig.Cmd)) > 60 || !strings.HasPrefix(sig.Cmd, "go test id ./...") {
		t.Fatalf("command not made safe: %q", sig.Cmd)
	}
}

func TestExcludedExit(t *testing.T) {
	for _, code := range []int{0, 124, 130, 137, 143, 75, 76, 77, 78, 255} {
		if !ExcludedExit(code) {
			t.Errorf("%d must be excluded", code)
		}
	}
	for _, code := range []int{1, 2, 3, 126, 127, 139} {
		if ExcludedExit(code) {
			t.Errorf("%d is a deterministic failure", code)
		}
	}
}
