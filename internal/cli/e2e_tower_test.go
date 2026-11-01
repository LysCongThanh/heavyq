package cli

import (
	"strings"
	"testing"

	"github.com/LysCongThanh/heavyq/internal/snapshot"
)

// The Tower's exceptions are facts computed from the ledger and the work
// trees: these tests run the real binary against real temp git repositories.
// "machine" exceptions depend on the host the tests run on, so they are
// ignored here.

func exceptionsOf(s snapshot.Snapshot, kind string) []snapshot.Exception {
	var out []snapshot.Exception
	for _, e := range s.Exceptions {
		if e.Kind == kind {
			out = append(out, e)
		}
	}
	return out
}

func TestTowerQuietQueueHasNoExceptions(t *testing.T) {
	h := newRig(t, baseConfig)
	s := h.statusJSON()
	if s.Exceptions == nil || s.Gates == nil {
		t.Fatalf("exceptions and gates must be [] on an idle queue, got %v %v", s.Exceptions, s.Gates)
	}
	for _, k := range []string{snapshot.KindRedGate, snapshot.KindLoop, snapshot.KindStarved} {
		if got := exceptionsOf(s, k); len(got) != 0 {
			t.Fatalf("%s on an idle queue: %+v", k, got)
		}
	}
}

func TestTowerRedGateFollowsTheTree(t *testing.T) {
	h := newRig(t, baseConfig)
	repo := gateRepo(t, h, "")
	h.edit(repo, "edited\n") // the middle command fails unless marker.txt is as committed
	if rc, out := h.run("gate", "run"); rc != 1 {
		t.Fatalf("gate run rc=%d\n%s", rc, out)
	}

	s := h.statusJSON()
	red := exceptionsOf(s, snapshot.KindRedGate)
	if len(red) != 1 || len(s.Gates) != 1 || s.Gates[0].State != snapshot.GateFailed || s.Gates[0].Gate != "ready" {
		t.Fatalf("gates=%+v exceptions=%+v", s.Gates, s.Exceptions)
	}
	e := red[0]
	if e.Lane != "app" || e.Severity != snapshot.SeverityCritical || e.Evidence.Count != 1 || e.Evidence.Tree != s.Gates[0].Tree ||
		!strings.Contains(e.Evidence.Cmd, "marker.txt") || e.Evidence.At.IsZero() ||
		e.Action == nil || e.Action.Kind != snapshot.ActionRun || !strings.HasSuffix(e.Action.Command, "heavyq gate run ready") {
		t.Fatalf("%+v action=%+v", e, e.Action)
	}
	if rc, out := h.run("why"); rc != 0 || !strings.Contains(out, "Needs you: app's ready gate fails on its current code") {
		t.Fatalf("why rc=%d\n%s", rc, out)
	}

	// Another tree has no failure on it: reverting the edit clears the card,
	// and the lane is merely unproven.
	h.edit(repo, "from-commit\n")
	s = h.statusJSON()
	if got := exceptionsOf(s, snapshot.KindRedGate); len(got) != 0 || len(s.Gates) != 1 || s.Gates[0].State != snapshot.GateUnproven {
		t.Fatalf("after revert: gates=%+v exceptions=%+v", s.Gates, got)
	}
}

func TestTowerLoopAndSkippedGate(t *testing.T) {
	h := newRig(t, baseConfig)
	gitRepo(t, h) // clean tree, no heavyq.toml
	for range 2 {
		if rc, out := h.run("3", "--lane", "agent", "--", "sh", "-c", "exit 3"); rc != 3 {
			t.Fatalf("rc=%d\n%s", rc, out)
		}
	}
	if rc, out := h.run("3", "--lane", "other", "--", "sh", "-c", "exit 3"); rc != 3 {
		t.Fatalf("rc=%d\n%s", rc, out)
	}

	s := h.statusJSON()
	loops := exceptionsOf(s, snapshot.KindLoop)
	if len(loops) != 1 {
		t.Fatalf("one lane failed twice, exceptions=%+v", s.Exceptions)
	}
	e := loops[0]
	if e.Lane != "agent" || e.Evidence.Count != 2 || e.Evidence.Tree == "" || !strings.Contains(e.Evidence.Cmd, "exit 3") ||
		!strings.Contains(e.Text, "2 times") {
		t.Fatalf("%+v", e)
	}
	// No heavyq.toml: the gate is skipped, and the snapshot says so.
	if len(s.Gates) == 0 {
		t.Fatalf("lanes ran in a work tree, gates=%+v", s.Gates)
	}
	for _, g := range s.Gates {
		if g.State != snapshot.GateSkipped || !strings.Contains(g.Reason, "no heavyq.toml") {
			t.Fatalf("gate %+v", g)
		}
	}
	if len(exceptionsOf(s, snapshot.KindRedGate)) != 0 {
		t.Fatalf("a skipped gate is not red: %+v", s.Exceptions)
	}
}
