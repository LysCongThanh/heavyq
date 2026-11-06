package cli

import (
	"os"
	"strings"

	"github.com/LysCongThanh/heavyq/internal/config"
	"github.com/LysCongThanh/heavyq/internal/gitutil"
	"github.com/LysCongThanh/heavyq/internal/history"
	"github.com/LysCongThanh/heavyq/internal/pools"
	"github.com/LysCongThanh/heavyq/internal/runident"
)

// checkoutFor says which code a run on machine sees.
//
// A remote run executes exactly the commit that was shipped, so it records
// that commit and its tree, never dirty, whatever the local work tree holds.
// A local run executes the files in place and records the work tree as it is
// now (clean or dirty). An empty machine (the run never started) counts as
// remote only when the target never runs here (one remote, or a list of
// remotes only).
//
// A directory outside git, or a git failure, gives an empty Checkout: the
// ledger line is still written, just without code identity.
func (r *run) checkoutFor(machine string) gitutil.Checkout {
	if machine == "" && !config.TargetMayRunLocally(r.pl.target) {
		machine = r.pl.target // not local: the shipped commit is what would have run
	}
	var (
		co  gitutil.Checkout
		err error
	)
	if machine != "" && machine != config.LocalName && r.pl.sha != "" {
		co, err = gitutil.Pinned(r.localDir(), r.pl.sha)
	} else {
		co, err = gitutil.Identify(r.localDir())
	}
	if err != nil {
		r.verbosef("ledger: code identity incomplete: %v", err)
	}
	return co
}

// settled is the identity to record for a local run that started on before
// and has just ended. If the work tree changed while the command ran, its
// result describes neither tree, so no tree is recorded (and the run can never
// match as proof); dirty then reflects the tree as it is now.
func (r *run) settled(before gitutil.Checkout) gitutil.Checkout {
	if before.Tree == "" {
		return before
	}
	if m := r.siblingDrift(); m != "" { // a sibling moved or was edited under the running command
		r.verbosef("ledger: a --with sibling changed during the run (%s); no tree identity recorded", m)
		before.Tree = ""
		return before
	}
	after, err := gitutil.Identify(r.localDir())
	if err != nil || after.Tree == before.Tree {
		return before
	}
	r.verbosef("ledger: the work tree changed during the run; no tree identity recorded")
	before.Tree, before.Dirty = "", after.Dirty
	return before
}

// runID is the run_id of this run on the code co: the same computation the
// cache key and `gate status` use, with the environment this process has.
func (r *run) runID(co gitutil.Checkout) string {
	return runident.ID(runident.Input{Tree: co.Tree, Argv: r.opts.Argv, Subdir: co.Subdir, EnvNames: r.cfg.Cache.Env,
		Getenv: os.LookupEnv, BaseSHA: runident.BaseSHA(r.localDir(), r.opts.BaseRef), Siblings: siblingIDs(r.pl.siblings)})
}

// tag stamps who ran it, what exactly it ran (the argv hash, taken before the
// display form of the command is redacted and cut) and on which code onto a
// ledger record.
func (r *run) tag(rec *history.Record, co gitutil.Checkout) {
	rec.CmdHash = history.CmdHash(r.opts.Argv)
	rec.RunID = r.runID(co)
	rec.Session, rec.Item, rec.Gate = r.opts.Session, r.opts.Item, r.opts.Gate
	rec.Agent, rec.Model = agentModelOf(co.Worktree, rec.Session) // from the session meta record, when there is one
	rec.Needs = strings.Join(pools.FormatNeeds(r.opts.Needs), ",")
	rec.With = strings.Join(siblingLabels(r.pl.siblings), ",")
	if r.me != nil { // a cache hit is recorded before the ticket exists
		rec.Pools = pools.FormatHeld(r.opts.Needs, r.me.Pools)
	}
	rec.Repo, rec.Worktree, rec.Subdir = co.Repo, co.Worktree, co.Subdir
	rec.Commit, rec.Tree, rec.Dirty = co.Commit, co.Tree, co.Dirty
}
