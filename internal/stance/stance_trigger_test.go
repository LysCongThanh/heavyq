package stance

import (
	"testing"
	"time"

	"github.com/LysCongThanh/heavyq/internal/gitutil"
	"github.com/LysCongThanh/heavyq/internal/history"
)

var t0 = time.Date(2026, 11, 2, 9, 0, 0, 0, time.UTC)

// run is a ledger record of lane "wt" at minute m of the test day.
func run(m int, cmd string, exit int, tree string, mod ...func(*history.Record)) history.Record {
	r := history.Record{End: t0.Add(time.Duration(m) * time.Minute), Lane: "wt", Repo: "r", Cmd: cmd, Exit: exit, Tree: tree, RunSecs: 30}
	for _, f := range mod {
		f(&r)
	}
	return r
}

func TestSameFailureClassAcrossTrees(t *testing.T) {
	underLoad := func(r *history.Record) { r.Cores, r.Load1, r.MaxLoad = 4, 9, 4 }
	cases := []struct {
		name string
		recs []history.Record
		want int
	}{
		{"none", nil, 0},
		{"one tree repeated is not across trees", []history.Record{run(1, "go test", 1, "a"), run(2, "go test", 1, "a")}, 1},
		{"three trees", []history.Record{run(1, "go test", 1, "a"), run(2, "go test", 1, "b"), run(3, "go test", 1, "c")}, 3},
		{"a pass of the command forgets the class", []history.Record{
			run(1, "go test", 1, "a"), run(2, "go test", 1, "b"), run(3, "go test", 0, "c"), run(4, "go test", 1, "d")}, 1},
		{"a pass of another command does not", []history.Record{
			run(1, "go test", 1, "a"), run(2, "go vet", 0, "b"), run(3, "go test", 1, "c")}, 2},
		{"another exit code is another class", []history.Record{
			run(1, "go test", 1, "a"), run(2, "go test", 2, "b"), run(3, "go test", 1, "c")}, 2},
		{"failures the machine explains do not count", []history.Record{
			run(1, "go test", 1, "a", underLoad), run(2, "go test", 1, "b", underLoad), run(3, "go test", 1, "c")}, 1},
		{"cache replays and give-ups do not count", []history.Record{
			run(1, "go test", 1, "a"),
			run(2, "go test", 1, "b", func(r *history.Record) { r.Cache = "hit" }),
			run(3, "go test", 1, "c", func(r *history.Record) { r.Result = history.OutcomeGaveUp })}, 1},
		{"unknown tree does not count", []history.Record{run(1, "go test", 1, ""), run(2, "go test", 1, "")}, 0},
		{"same command in another repo is another class", []history.Record{
			run(1, "go test", 1, "a"), run(2, "go test", 1, "b", func(r *history.Record) { r.Repo = "other" })}, 1},
	}
	for _, c := range cases {
		got := SameFailureClassAcrossTrees(c.recs)
		if got.Trees != c.want {
			t.Errorf("%s: %d trees (%q), want %d", c.name, got.Trees, got.Label, c.want)
		}
	}
	if got := SameFailureClassAcrossTrees([]history.Record{run(1, "go test ./...", 1, "a"), run(2, "go test ./...", 1, "b")}); got.Label != "go test ./... exit 1" {
		t.Errorf("label = %q", got.Label)
	}
}

func commit(subject string, files ...string) gitutil.CommitFiles {
	return gitutil.CommitFiles{Subject: subject, Files: files}
}

func TestChurnReverts(t *testing.T) {
	cases := []struct {
		name                    string
		newestFirst             []gitutil.CommitFiles
		reverts, rewrites, want int
	}{
		{"none", nil, 0, 0, 0},
		{"distinct work is not churn", []gitutil.CommitFiles{commit("c", "c.go"), commit("b", "b.go"), commit("a", "a.go")}, 0, 0, 0},
		{"a revert", []gitutil.CommitFiles{commit(`Revert "add a"`, "a.go"), commit("add a", "a.go")}, 1, 0, 1},
		{"redo of the same files", []gitutil.CommitFiles{commit("again", "a.go", "b.go"), commit("first", "b.go", "a.go")}, 0, 1, 1},
		{"revert then reapply", []gitutil.CommitFiles{commit("Reapply", "a.go"), commit(`Revert "x"`, "a.go"), commit("x", "a.go")}, 1, 1, 2},
		{"overlap alone is not a rewrite", []gitutil.CommitFiles{commit("b", "a.go", "b.go"), commit("a", "a.go")}, 0, 0, 0},
		{"commits without files are skipped", []gitutil.CommitFiles{commit("empty"), commit("empty")}, 0, 0, 0},
	}
	for _, c := range cases {
		got := ChurnReverts(c.newestFirst)
		if got.Reverts != c.reverts || got.Rewrites != c.rewrites || got.Count() != c.want {
			t.Errorf("%s: %+v, want %d reverts %d rewrites", c.name, got, c.reverts, c.rewrites)
		}
	}
	if got := ChurnReverts([]gitutil.CommitFiles{commit(`Revert "x"`, "z.go", "a.go")}); len(got.Files) != 2 || got.Files[0] != "a.go" {
		t.Errorf("files = %v, want sorted", got.Files)
	}
}

func TestMatchKeyword(t *testing.T) {
	kws := []string{"breakthrough", "think differently", "đột phá"}
	cases := []struct {
		prompt string
		want   string
	}{
		{"we need a Breakthrough here", "breakthrough"},
		{"Think   differently", ""}, // whitespace is not normalised: the phrase is literal
		{"please think differently about this", "think differently"},
		{"tôi cần một giải pháp đột phá cho vấn đề này", "đột phá"},
		{"ĐỘT PHÁ!", "đột phá"},
		{"breakthroughs everywhere", ""}, // whole words only
		{"un-breakthrough", "breakthrough"},
		{"đột phá2", ""},
		{"nothing to see", ""},
		{"", ""},
	}
	for _, c := range cases {
		got, ok := MatchKeyword(c.prompt, kws)
		if got != c.want || ok != (c.want != "") {
			t.Errorf("%q: %q, %v; want %q", c.prompt, got, ok, c.want)
		}
	}
	if _, ok := MatchKeyword("x", []string{"", "  "}); ok {
		t.Error("an empty keyword must never match")
	}
	long := make([]byte, maxPromptScan+100)
	for i := range long {
		long[i] = 'a'
	}
	if _, ok := MatchKeyword(string(long)+" breakthrough", kws); ok {
		t.Error("a keyword past the scan limit must not match")
	}
}

func TestEvaluate(t *testing.T) {
	bt := Stance{Name: "breakthrough", Enter: Enter{SameFailureClassAcrossTrees: 3, ChurnReverts: 2, Keywords: []string{"breakthrough"}}}
	calm := Stance{Name: "calm", Enter: Enter{Keywords: []string{"calm down"}, ChurnReverts: 5}}
	stallOnly := Stance{Name: "stall", Enter: Enter{Stall: &Stall{Minutes: 1, Runs: 1, WithoutProof: true}}}
	all := []Stance{bt, calm, stallOnly}
	fail3 := FailureClass{Label: "go test exit 1", Trees: 3}
	cases := []struct {
		name string
		ev   Evidence
		want []string // "stance/trigger"
	}{
		{"nothing", Evidence{}, nil},
		{"keyword", Evidence{Prompt: "breakthrough please"}, []string{"breakthrough/keyword"}},
		{"failure below threshold", Evidence{Failure: FailureClass{Trees: 2}}, nil},
		{"failure at threshold", Evidence{Failure: fail3}, []string{"breakthrough/same_failure_class_across_trees"}},
		{"churn", Evidence{Churn: Churn{Reverts: 1, Rewrites: 1}}, []string{"breakthrough/churn_reverts"}},
		{"keyword beats evidence", Evidence{Prompt: "breakthrough", Failure: fail3}, []string{"breakthrough/keyword"}},
		{"keyword of one, evidence of another, keyword first", Evidence{Prompt: "calm down", Failure: fail3},
			[]string{"calm/keyword", "breakthrough/same_failure_class_across_trees"}},
		{"stall alone never matches", Evidence{Prompt: "stall"}, nil},
	}
	for _, c := range cases {
		var got []string
		for _, m := range Evaluate(all, c.ev) {
			got = append(got, m.Stance+"/"+string(m.Trigger))
			if m.Why == "" {
				t.Errorf("%s: empty why for %v", c.name, m)
			}
		}
		if len(got) != len(c.want) {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("%s: %v, want %v", c.name, got, c.want)
			}
		}
	}
	if f, c := NeedsEvidence([]Stance{{Enter: Enter{Keywords: []string{"x"}}}}); f || c {
		t.Error("keywords only need no evidence")
	}
}
