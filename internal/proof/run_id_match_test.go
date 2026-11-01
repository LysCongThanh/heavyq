package proof

import (
	"strings"
	"testing"

	"github.com/LysCongThanh/heavyq/internal/history"
	"github.com/LysCongThanh/heavyq/internal/runident"
)

// idFor is a Subject.RunID for tests: the real run_id of argv on treeA with
// FEATURE_X as the allowlisted variable, valued env ("" = unset).
func idFor(env string) func([]string) string {
	return func(argv []string) string {
		get := func(string) (string, bool) { return "", false }
		if env != "" {
			get = func(string) (string, bool) { return env, true }
		}
		return runident.ID(runident.Input{Tree: treeA, Argv: argv, EnvNames: []string{"FEATURE_X"}, Getenv: get})
	}
}

// withID is a record of cmd that carries the run_id it ran under (env = value of FEATURE_X).
func withID(cmd, env string, mod func(*history.Record)) history.Record {
	return run(cmd, func(r *history.Record) {
		r.RunID = idFor(env)(Argv(cmd))
		if mod != nil {
			mod(r)
		}
	})
}

// The hole of #46: a pass under FEATURE_X=1 must not prove the command asked
// about without it, and the other way round.
func TestRunIDSeparatesRunsByEnvironment(t *testing.T) {
	cmd := "go test ./..."
	for _, c := range []struct{ name, ran, asked string }{
		{"passed with the variable, asked without", "1", ""},
		{"passed without, asked with the variable", "", "1"},
		{"passed with another value", "2", "1"},
	} {
		t.Run(c.name, func(t *testing.T) {
			subj := Subject{Repo: repoA, Tree: treeA, RunID: idFor(c.asked)}
			g := Evaluate("ready", []string{cmd}, subj, []history.Record{withID(cmd, c.ran, nil)})
			if g.Proven || g.Commands[0].State != Missing || !strings.Contains(g.Commands[0].Reason, "different environment") {
				t.Fatalf("%+v", g.Commands[0])
			}
		})
	}
	subj := Subject{Repo: repoA, Tree: treeA, RunID: idFor("1")}
	if g := Evaluate("ready", []string{cmd}, subj, []history.Record{withID(cmd, "1", nil)}); !g.Proven {
		t.Fatalf("the same environment must prove: %+v", g.Commands[0])
	}
}

// A failure under another environment is not a failure of this one, and it
// does not make this one flaky.
func TestRunIDKeepsFailuresOfOtherEnvironmentsApart(t *testing.T) {
	cmd := "go test ./..."
	subj := Subject{Repo: repoA, Tree: treeA, RunID: idFor("")}
	recs := []history.Record{
		withID(cmd, "1", func(r *history.Record) { r.Exit = 1 }),
		withID(cmd, "", nil),
	}
	if g := Evaluate("ready", []string{cmd}, subj, recs); !g.Proven || g.Commands[0].Fails != 0 {
		t.Fatalf("%+v", g.Commands[0])
	}
}

// Older records (no run_id) are matched by cmd_hash only when the allowlist
// is empty; with one they could have run with any value, so they never match.
func TestRecordsWithoutRunIDFailClosedWhenEnvCouldDiffer(t *testing.T) {
	cmd := "go vet ./..."
	old := run(cmd, nil) // cmd_hash, no run_id
	envFree := Subject{Repo: repoA, Tree: treeA, EnvFree: true, RunID: idFor("")}
	if g := Evaluate("ready", []string{cmd}, envFree, []history.Record{old}); !g.Proven {
		t.Fatalf("no allowlist: the old rules apply, got %+v", g.Commands[0])
	}
	strict := Subject{Repo: repoA, Tree: treeA, RunID: idFor("")}
	g := Evaluate("ready", []string{cmd}, strict, []history.Record{old})
	if g.Proven || !strings.Contains(g.Commands[0].Reason, "no run id") {
		t.Fatalf("allowlist set: an old record must not prove, got %+v", g.Commands[0])
	}
	// the zero Subject fails closed too
	if g := Evaluate("ready", []string{cmd}, Subject{Repo: repoA, Tree: treeA}, []history.Record{old}); g.Proven {
		t.Fatal("zero-value Subject must fail closed")
	}
}

// A Subject that cannot compute an id falls back to the old rules (EnvFree)
// even for records that carry one.
func TestSubjectWithoutRunIDFallsBackToCmdHash(t *testing.T) {
	cmd := "go vet ./..."
	r := withID(cmd, "1", nil)
	if g := Evaluate("ready", []string{cmd}, Subject{Repo: repoA, Tree: treeA, EnvFree: true}, []history.Record{r}); !g.Proven {
		t.Fatalf("%+v", g.Commands[0])
	}
	if g := Evaluate("ready", []string{cmd}, Subject{Repo: repoA, Tree: treeA}, []history.Record{r}); g.Proven {
		t.Fatal("without EnvFree and without an id, nothing can be proven")
	}
}

func TestMatcherForUsesRunID(t *testing.T) {
	cmd := "make check"
	m := MatcherFor(cmd, Subject{Repo: repoA, Tree: treeA, RunID: idFor("1")})
	if !m(withID(cmd, "1", nil)) || m(withID(cmd, "", nil)) {
		t.Fatal("MatcherFor must follow the run_id")
	}
}
