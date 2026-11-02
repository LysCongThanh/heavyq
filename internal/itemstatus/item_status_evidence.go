package itemstatus

import (
	"time"

	"github.com/LysCongThanh/heavyq/internal/history"
	"github.com/LysCongThanh/heavyq/internal/proof"
)

// NoProof is the Evidence of an item that declares no proof.
func NoProof() Evidence { return Evidence{Reason: "no proof declared"} }

// Unprovable is the Evidence of an item whose proof cannot be evaluated at
// all (its gate does not exist, say). Reason says why.
func Unprovable(reason string) Evidence { return Evidence{Declared: true, Reason: reason} }

// EvidenceFor turns the evaluation g of an item's commands on the code in
// subject into Evidence. recs is the ledger, oldest first; it is only used to
// find out whether the proof passed on an older tree of the same repository.
func EvidenceFor(g proof.Gate, subject proof.Subject, recs []history.Record) Evidence {
	ev := Evidence{Declared: true, Proven: g.Proven, Commit: ShortCommit(g.Commit), Dirty: g.Dirty}
	older := &Run{}
	haveOlder := len(g.Commands) > 0
	for _, c := range g.Commands {
		if c.At.After(ev.At) {
			ev.At = c.At
		}
		if c.State == proof.Failed {
			ev.Failed = true
		}
		if c.State != proof.Proven && ev.Reason == "" {
			ev.Reason = commandReason(c)
		}
		if c.State == proof.Proven {
			continue
		}
		r, ok := lastPassOnOtherTree(c.Cmd, subject, recs)
		if !ok {
			haveOlder = false
			continue
		}
		if older.At.IsZero() || r.End.Before(older.At) { // the gate was whole only since its oldest part
			older.At, older.Commit = r.End, ShortCommit(r.Commit)
		}
	}
	if ev.Proven {
		ev.Reason = ""
	} else {
		ev.Commit, ev.At, ev.Dirty = "", time.Time{}, false
		if ev.Reason == "" {
			ev.Reason = g.Reason
		}
		if haveOlder && !ev.Failed {
			ev.Older = older
		}
	}
	return ev
}

func commandReason(c proof.Command) string {
	switch {
	case c.State == proof.Failed && c.Class == proof.ClassFlaky:
		return "flaky on this code: " + c.Cmd
	case c.State == proof.Failed:
		return "failed on this code: " + c.Cmd
	case c.Reason != "":
		return c.Reason
	}
	return "not run on this code yet: " + c.Cmd
}

// lastPassOnOtherTree is the latest run of cmd that ran to its end and passed
// on a tree other than subject's, in the same repository, from its root. It
// matches on cmd_hash: the run_id names the tree, so it cannot tell the same
// command on another tree. Runs without a hash (before heavyq 0.4) are ignored.
func lastPassOnOtherTree(cmd string, s proof.Subject, recs []history.Record) (history.Record, bool) {
	if s.Repo == "" || s.Tree == "" { // code nobody can name has no "older" either
		return history.Record{}, false
	}
	want := history.CmdHash(proof.Argv(cmd))
	for i := len(recs) - 1; i >= 0; i-- {
		r := recs[i]
		if r.CmdHash == want && r.Repo == s.Repo && r.Tree != "" && r.Tree != s.Tree &&
			r.Subdir == "" && r.Result == "" && r.Exit == 0 {
			return r, true
		}
	}
	return history.Record{}, false
}
