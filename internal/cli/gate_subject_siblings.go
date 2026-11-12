package cli

import (
	"github.com/LysCongThanh/heavyq/internal/config"
	"github.com/LysCongThanh/heavyq/internal/proof"
	"github.com/LysCongThanh/heavyq/internal/repoconfig"
)

// gateSubject is the proof subject for gate g in the repository at root. A
// gate with `with` siblings runs against them pinned (HEAD, or the ref it
// names, resolved now), exactly as `gate run` passes them to the run, so the
// run_id it looks for carries the same sibling commits. When a sibling cannot
// be resolved nothing is proven: the subject has no tree and says why (a
// record must never match by command alone, ignoring the siblings). A gate
// with `paths` is identified by the files they name, not by the whole tree.
func gateSubject(cfg *config.Config, root string, g repoconfig.Gate) proof.Subject {
	if len(g.With) == 0 {
		return subjectOfWith(cfg, root, g.Paths, nil)
	}
	sibs, err := resolveSiblings(g.With, root)
	if err != nil {
		s := subjectOfWith(cfg, root, g.Paths, nil)
		s.Tree, s.RunID, s.Why = "", nil, err.Error()
		return s
	}
	return subjectOfWith(cfg, root, g.Paths, siblingIDs(sibs))
}
