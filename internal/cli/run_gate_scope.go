package cli

import (
	"github.com/LysCongThanh/heavyq/internal/gitutil"
	"github.com/LysCongThanh/heavyq/internal/repoconfig"
)

// gatePaths is the `paths` of the gate this run proves (--gate), the files its tree identity is made of; nil for
// a run that proves no gate, and for a gate without paths (the whole tree). It is the file `gate status` reads,
// so a run is recorded on the very identity the gate is later looked up by. A heavyq.toml that cannot be read
// scopes nothing: `gate status` fails on it as well, and a whole-tree record never proves a scoped gate.
func (r *run) gatePaths() []string {
	if !r.scopeRead {
		r.scopeRead = true
		r.scope = gatePathsOf(r.localDir(), r.opts.Gate)
	}
	return r.scope
}

// gatePathsOf is the `paths` of gate name in the heavyq.toml of the repository around dir.
func gatePathsOf(dir, name string) []string {
	if name == "" {
		return nil
	}
	repo, err := repoconfig.Load(dir)
	if err != nil || repo == nil {
		return nil
	}
	g, ok := repo.Gate(name)
	if !ok {
		return nil
	}
	return g.Paths
}

// wholeTree is the id of the whole tree of the code co was identified on, on machine, or "" when co has no tree
// (a local run whose work tree changed under it). A scoped run is recorded on the scoped id, but the result
// cache is keyed by, and a replayed result proves, the whole tree: a store or a replay is compared on that one.
func (r *run) wholeTree(machine string, co gitutil.Checkout) string {
	if co.Tree == "" || len(r.gatePaths()) == 0 {
		return co.Tree
	}
	return r.checkoutOn(machine, nil).Tree
}
