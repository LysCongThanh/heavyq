package cli

import (
	"fmt"
	"io"

	"github.com/LysCongThanh/heavyq/internal/anchor"
	"github.com/LysCongThanh/heavyq/internal/history"
	"github.com/LysCongThanh/heavyq/internal/itemstatus"
	"github.com/LysCongThanh/heavyq/internal/proof"
	"github.com/LysCongThanh/heavyq/internal/repoconfig"
)

// evidenceCache evaluates the proofs of items against one ledger. Many items
// share a gate and many are judged on the same tree, so each (tree, proof) is
// evaluated once.
type evidenceCache struct {
	set  *itemSet
	recs []history.Record
	done map[string]itemstatus.Evidence
}

func newEvidenceCache(s *itemSet, recs []history.Record) *evidenceCache {
	return &evidenceCache{set: s, recs: recs, done: map[string]itemstatus.Evidence{}}
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
	var name string
	var commands []string
	switch p.Kind {
	case anchor.ProofGate:
		g, ok := proofGate(c.set, p.Value)
		if !ok {
			return itemstatus.Unprovable(fmt.Sprintf("no gate %q in heavyq.toml", p.Value))
		}
		name, commands = g.Name, g.Run
	case anchor.ProofCmd:
		name, commands = "item", []string{p.Value}
	default:
		return itemstatus.Unprovable("unknown proof kind " + p.Kind)
	}
	subj := c.set.subject(root)
	return itemstatus.EvidenceFor(proof.Evaluate(name, commands, subj, c.recs), subj, c.recs)
}

// proofGate is the gate called name in the repository's heavyq.toml (or the
// detected default gate).
func proofGate(s *itemSet, name string) (repoconfig.Gate, bool) {
	if s.repo == nil {
		return repoconfig.Gate{}, false
	}
	return s.repo.Gate(name)
}

// ledgerOrNone reads the proof ledger; an unreadable one proves nothing, and
// the caller hears why on stderr.
func ledgerOrNone(stderr io.Writer) []history.Record {
	recs, err := readProofLedger()
	if err != nil {
		fmt.Fprintln(stderr, "heavyq: ledger unreadable, so nothing can be proven:", err)
		return nil
	}
	return recs
}
