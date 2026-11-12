package cli

import (
	"github.com/LysCongThanh/heavyq/internal/config"
	"github.com/LysCongThanh/heavyq/internal/gitutil"
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
//
// Nothing is proven either while a sibling pinned to its HEAD has uncommitted
// edits next to the repository: the proof names the sibling's commit, but a
// local run of the gate reads ../<sibling> with the edits, which nothing ran.
// (The main repository's own edits are part of its tree id; a sibling's
// identity is its commit, so the edits must be committed first.) A sibling
// pinned to an explicit ref does not depend on its work tree.
func gateSubject(cfg *config.Config, root string, g repoconfig.Gate) proof.Subject {
	if len(g.With) == 0 {
		return subjectOfWith(cfg, root, g.Paths, nil)
	}
	sibs, err := resolveSiblings(g.With, root)
	if err != nil {
		return unprovable(cfg, root, g.Paths, err.Error())
	}
	for i, spec := range g.With {
		if _, ref, _ := config.ParseSiblingSpec(spec); ref == "HEAD" && sibs[i].NextToMain && !gitutil.IsClean(sibs[i].Path) {
			return unprovable(cfg, root, g.Paths, sibs[i].Name+" has uncommitted changes: the gate proves its commit, so commit them (or stash them) first")
		}
	}
	return subjectOfWith(cfg, root, g.Paths, siblingIDs(sibs))
}

// unprovable is the subject of root (scoped to a gate's paths) on which
// nothing can be proven, and why.
func unprovable(cfg *config.Config, root string, scope []string, why string) proof.Subject {
	s := subjectOfWith(cfg, root, scope, nil)
	s.Tree, s.RunID, s.Why = "", nil, why
	return s
}
