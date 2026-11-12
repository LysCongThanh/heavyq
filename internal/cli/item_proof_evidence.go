package cli

import (
	"fmt"
	"io"
	"time"

	"github.com/LysCongThanh/heavyq/internal/anchor"
	"github.com/LysCongThanh/heavyq/internal/history"
	"github.com/LysCongThanh/heavyq/internal/itemstatus"
	"github.com/LysCongThanh/heavyq/internal/ledgerindex"
	"github.com/LysCongThanh/heavyq/internal/proof"
	"github.com/LysCongThanh/heavyq/internal/repoconfig"
)

// itemStaleWindow is how far back a passing run on an older tree still makes
// an item "stale" rather than "open".
const itemStaleWindow = 7 * 24 * time.Hour

// evidenceCache evaluates the proofs of items. The records that decide a proof
// on a tree come from the tree's index (proofRecords); recent, the runs of
// the last itemStaleWindow, only tell whether the proof passed on an older
// tree. Many items share a gate and many are judged on the same tree, so each
// (tree, proof) is evaluated once.
type evidenceCache struct {
	set    *itemSet
	recent []history.Record
	done   map[string]itemstatus.Evidence
}

func newEvidenceCache(s *itemSet, recent []history.Record) *evidenceCache {
	return &evidenceCache{set: s, recent: recent, done: map[string]itemstatus.Evidence{}}
}

// of is the proof state of it on the code in work tree root.
func (c *evidenceCache) of(it anchor.Item, root string) itemstatus.Evidence {
	if it.Proof.Kind == "" {
		return itemstatus.NoProof()
	}
	key := root + "\x00" + it.Proof.Kind + "\x00" + it.Proof.Value
	if ev, ok := c.done[key]; ok {
		return ev
	}
	ev := c.evaluate(it.Proof, root)
	c.done[key] = ev
	return ev
}

func (c *evidenceCache) evaluate(p anchor.Proof, root string) itemstatus.Evidence {
	name, commands, why := proofCommands(c.set, p)
	if why != "" {
		return itemstatus.Unprovable(why)
	}
	subj := c.set.proofSubject(root, p)
	recs, err := proofRecords(subj)
	if err != nil { // fail closed: an unreadable ledger proves nothing
		recs = nil
	}
	return itemstatus.EvidenceFor(proof.Evaluate(name, commands, subj, recs), subj, c.recent)
}

// proofCommands is the gate name and the shell commands a proof stands for. why
// is set instead when the proof cannot be evaluated at all (its gate does not
// exist, or its kind is unknown).
func proofCommands(s *itemSet, p anchor.Proof) (name string, commands []string, why string) {
	switch p.Kind {
	case anchor.ProofGate:
		g, ok := proofGate(s, p.Value)
		if !ok {
			return "", nil, fmt.Sprintf("no gate %q in heavyq.toml", p.Value)
		}
		return g.Name, g.Run, ""
	case anchor.ProofCmd:
		return "item", []string{p.Value}, ""
	}
	return "", nil, "unknown proof kind " + p.Kind
}

// proofGate is the gate called name in the repository's heavyq.toml (or the
// detected default gate).
func proofGate(s *itemSet, name string) (repoconfig.Gate, bool) {
	if s.repo == nil {
		return repoconfig.Gate{}, false
	}
	return s.repo.Gate(name)
}

// ledgerOrNone reads the recent runs (lane index, else the ledger tail) used
// to find a lane's work tree and older passing runs. An unreadable ledger
// proves nothing; the caller hears why on stderr.
func ledgerOrNone(stderr io.Writer) []history.Record {
	recs, _, err := ledgerindex.Recent(history.DefaultPath(), time.Now().Add(-itemStaleWindow))
	if err != nil {
		fmt.Fprintln(stderr, "heavyq: ledger unreadable, so nothing can be proven:", err)
		return nil
	}
	return recs
}
