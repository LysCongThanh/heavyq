package proof

import (
	"strings"
	"testing"

	"github.com/LysCongThanh/heavyq/internal/history"
)

func TestMatchesByHashWhenTheRecordHasOne(t *testing.T) {
	cmd := "go vet ./..."
	subj := Subject{Repo: repoA, Tree: treeA}
	cases := []struct {
		name string
		r    history.Record
		want State
	}{
		{"hash and the same string", run(cmd, nil), Proven},
		{"hash with another display string still matches", run(cmd, func(r *history.Record) { r.Cmd = "something else" }), Proven},
		{"another hash does not match even with the same string", run(cmd, func(r *history.Record) { r.CmdHash = history.CmdHash(Argv("go build")) }), Missing},
		{"no hash falls back to the string", run(cmd, func(r *history.Record) { r.CmdHash = "" }), Proven},
		{"no hash and another string", run(cmd, func(r *history.Record) { r.CmdHash, r.Cmd = "", "sh -c go build" }), Missing},
		{"hash of the bare command is not the hash of the shell run", run(cmd, func(r *history.Record) { r.CmdHash = history.CmdHash([]string{cmd}) }), Missing},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Evaluate("ready", []string{cmd}, subj, []history.Record{c.r}).Commands[0].State; got != c.want {
				t.Fatalf("state = %s, want %s", got, c.want)
			}
		})
	}
}

// #38: a command whose display form the ledger cuts at 200 bytes is provable
// by its hash, and another long command with the same first 200 bytes is not
// taken for it.
func TestLongCommandIsProvableByHash(t *testing.T) {
	prefix := "echo " + strings.Repeat("x", 300)
	long, other := prefix+" one", prefix+" two"
	r := run(long, nil)
	if len(r.Cmd) != 200 {
		t.Fatalf("the ledger form must be cut, got %d bytes", len(r.Cmd))
	}
	subj := Subject{Repo: repoA, Tree: treeA}
	g := Evaluate("ready", []string{long, other}, subj, []history.Record{r})
	if g.Commands[0].State != Proven || g.Commands[1].State != Missing || g.Proven {
		t.Fatalf("%+v", g.Commands)
	}
	// The one that no run proved is missing for the right reason: the hash
	// found nothing, and nothing older could be compared by text.
	if !strings.Contains(g.Commands[1].Reason, "run it again") {
		t.Fatalf("reason: %q", g.Commands[1].Reason)
	}
	// An old record (no hash) of a long command proves nothing: it only has the cut form.
	old := run(long, func(r *history.Record) { r.CmdHash = "" })
	if g := Evaluate("ready", []string{long}, subj, []history.Record{old}); g.Proven {
		t.Fatal("a cut-off string must never prove a long command")
	}
}

// Failures of a long command classify like any other.
func TestLongCommandCanBeFlaky(t *testing.T) {
	long := "echo " + strings.Repeat("y", 400)
	runs := []history.Record{run(long, nil), run(long, exit(1))}
	c := Evaluate("ready", []string{long}, Subject{Repo: repoA, Tree: treeA}, runs).Commands[0]
	if c.Class != ClassFlaky {
		t.Fatalf("%+v", c)
	}
}

// A command with a secret matches by hash even though the ledger redacted it,
// and the hash is of the argv as run, not of the redacted text.
func TestSecretCommandMatchesByHashNotByRedactedText(t *testing.T) {
	cmd := "API_TOKEN=abc123 make check"
	r := run(cmd, nil)
	if strings.Contains(r.Cmd, "abc123") {
		t.Fatalf("the display form leaks: %q", r.Cmd)
	}
	other := run("API_TOKEN=zzz999 make check", nil) // redacts to the same display form
	if r.Cmd != other.Cmd {
		t.Skipf("redaction no longer makes these look alike (%q vs %q)", r.Cmd, other.Cmd)
	}
	g := Evaluate("ready", []string{cmd}, Subject{Repo: repoA, Tree: treeA}, []history.Record{other})
	if g.Proven {
		t.Fatal("a run of the command with another secret must not prove this one")
	}
	if !Evaluate("ready", []string{cmd}, Subject{Repo: repoA, Tree: treeA}, []history.Record{r}).Proven {
		t.Fatal("the run of the same command proves it")
	}
}
