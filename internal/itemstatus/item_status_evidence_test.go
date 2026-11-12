package itemstatus

import (
	"strings"
	"testing"
	"time"

	"github.com/LysCongThanh/heavyq/internal/history"
	"github.com/LysCongThanh/heavyq/internal/proof"
)

func rec(cmd, tree, commit string, exit int, at time.Time) history.Record {
	return history.Record{End: at, Cmd: "sh -c " + cmd, CmdHash: history.CmdHash(proof.Argv(cmd)),
		Exit: exit, Repo: "repo", Tree: tree, Commit: commit}
}

func TestEvidenceForGate(t *testing.T) {
	now := t0
	subj := proof.Subject{Repo: "repo", Tree: "tree-now", Commit: "0123456789abcdef"}
	cmds := []string{"go vet ./...", "go test ./..."}
	passNow := func(c string) proof.Command {
		return proof.Command{Cmd: c, State: proof.Proven, At: now.Add(-time.Minute)}
	}
	t.Run("proven", func(t *testing.T) {
		g := proof.Gate{Gate: "ready", Proven: true, Tree: "tree-now", Commit: subj.Commit, Dirty: true,
			Commands: []proof.Command{passNow(cmds[0]), passNow(cmds[1])}}
		ev := EvidenceFor(g, subj, nil)
		if !ev.Proven || ev.Commit != "0123456" || !ev.Dirty || ev.At.IsZero() || ev.Reason != "" || ev.Older != nil {
			t.Fatalf("ev = %+v", ev)
		}
	})
	t.Run("missing, never proven anywhere", func(t *testing.T) {
		g := proof.Gate{Commands: []proof.Command{{Cmd: cmds[0], State: proof.Missing}}}
		ev := EvidenceFor(g, subj, nil)
		if ev.Proven || ev.Older != nil || ev.Failed || ev.Reason == "" || ev.Commit != "" {
			t.Fatalf("ev = %+v", ev)
		}
	})
	t.Run("stale: every command passed on older trees, the oldest part counts", func(t *testing.T) {
		g := proof.Gate{Commands: []proof.Command{{Cmd: cmds[0], State: proof.Missing}, {Cmd: cmds[1], State: proof.Missing}}}
		recs := []history.Record{
			rec(cmds[0], "tree-1", "aaaaaaaaaa", 0, now.Add(-3*time.Hour)),
			rec(cmds[0], "tree-2", "bbbbbbbbbb", 0, now.Add(-2*time.Hour)), // latest pass of cmd 0
			rec(cmds[1], "tree-2", "bbbbbbbbbb", 0, now.Add(-90*time.Minute)),
			rec(cmds[1], "tree-3", "cccccccccc", 1, now.Add(-time.Hour)), // a failure on an older tree does not count
		}
		ev := EvidenceFor(g, subj, recs)
		if ev.Older == nil || ev.Older.Commit != "bbbbbbb" || !ev.Older.At.Equal(now.Add(-2*time.Hour)) {
			t.Fatalf("older = %+v", ev.Older)
		}
	})
	t.Run("not stale when one command never passed before", func(t *testing.T) {
		g := proof.Gate{Commands: []proof.Command{{Cmd: cmds[0], State: proof.Missing}, {Cmd: cmds[1], State: proof.Missing}}}
		recs := []history.Record{rec(cmds[0], "tree-1", "aaaaaaaaaa", 0, now.Add(-time.Hour))}
		if ev := EvidenceFor(g, subj, recs); ev.Older != nil {
			t.Fatalf("older = %+v", ev.Older)
		}
	})
	t.Run("a current failure is red, not stale", func(t *testing.T) {
		g := proof.Gate{Commands: []proof.Command{{Cmd: cmds[0], State: proof.Failed, Class: proof.ClassFailed}}}
		recs := []history.Record{rec(cmds[0], "tree-1", "aaaaaaaaaa", 0, now.Add(-time.Hour))}
		ev := EvidenceFor(g, subj, recs)
		if !ev.Failed || ev.Older != nil || ev.Reason != "failed on this code: "+cmds[0] {
			t.Fatalf("ev = %+v", ev)
		}
	})
	t.Run("a passing command and an older pass for the other", func(t *testing.T) {
		g := proof.Gate{Commands: []proof.Command{passNow(cmds[0]), {Cmd: cmds[1], State: proof.Missing}}}
		recs := []history.Record{rec(cmds[1], "tree-1", "aaaaaaaaaa", 0, now.Add(-time.Hour))}
		if ev := EvidenceFor(g, subj, recs); ev.Older == nil || ev.Older.Commit != "aaaaaaa" {
			t.Fatalf("ev = %+v", ev)
		}
	})
	t.Run("another repository, other directory and unfinished runs never count", func(t *testing.T) {
		g := proof.Gate{Commands: []proof.Command{{Cmd: cmds[0], State: proof.Missing}}}
		other := rec(cmds[0], "tree-1", "aaaaaaaaaa", 0, now.Add(-time.Hour))
		other.Repo = "elsewhere"
		sub := rec(cmds[0], "tree-1", "aaaaaaaaaa", 0, now.Add(-time.Hour))
		sub.Subdir = "pkg"
		gaveUp := rec(cmds[0], "tree-1", "aaaaaaaaaa", 0, now.Add(-time.Hour))
		gaveUp.Result = history.OutcomeGaveUp
		noHash := rec(cmds[0], "tree-1", "aaaaaaaaaa", 0, now.Add(-time.Hour))
		noHash.CmdHash = ""
		if ev := EvidenceFor(g, subj, []history.Record{other, sub, gaveUp, noHash}); ev.Older != nil {
			t.Fatalf("older = %+v", ev.Older)
		}
	})
	t.Run("unknown tree has no older either", func(t *testing.T) {
		g := proof.Gate{Commands: []proof.Command{{Cmd: cmds[0], State: proof.Missing}}}
		recs := []history.Record{rec(cmds[0], "tree-1", "aaaaaaaaaa", 0, now.Add(-time.Hour))}
		if ev := EvidenceFor(g, proof.Subject{Repo: "repo"}, recs); ev.Older != nil {
			t.Fatalf("older = %+v", ev.Older)
		}
	})
}

// A reason that names a proof command is shown by item ls --json, item done
// and recap: the command in it goes through the redactor, like proof.value.
func TestEvidenceReasonRedactsTheCommand(t *testing.T) {
	const cmd = "./check --token=hunter2hunter2"
	subj := proof.Subject{Repo: "repo", Tree: "tree-now"}
	for name, c := range map[string]proof.Command{
		"not run yet": {Cmd: cmd, State: proof.Missing},
		"failed":      {Cmd: cmd, State: proof.Failed, Class: proof.ClassFailed},
		"flaky":       {Cmd: cmd, State: proof.Failed, Class: proof.ClassFlaky},
	} {
		ev := EvidenceFor(proof.Gate{Commands: []proof.Command{c}}, subj, nil)
		if strings.Contains(ev.Reason, "hunter2") || !strings.Contains(ev.Reason, "./check --token=***") {
			t.Errorf("%s: reason = %q", name, ev.Reason)
		}
	}
}
