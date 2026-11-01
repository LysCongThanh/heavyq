package cli

import (
	"github.com/LysCongThanh/heavyq/internal/config"
	"github.com/LysCongThanh/heavyq/internal/gitutil"
	"github.com/LysCongThanh/heavyq/internal/history"
)

// checkoutFor says which code a run on machine sees.
//
// A remote run executes exactly the commit that was shipped, so it records
// that commit and its tree, never dirty, whatever the local work tree holds.
// A local run executes the files in place and records the work tree as it is
// now (clean or dirty). An empty machine (the run never started) counts as
// remote only when one specific remote was asked for.
//
// A directory outside git, or a git failure, gives an empty Checkout: the
// ledger line is still written, just without code identity.
func (r *run) checkoutFor(machine string) gitutil.Checkout {
	if machine == "" && r.pl.target != "local" && r.pl.target != "auto" {
		machine = r.pl.target
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
	after, err := gitutil.Identify(r.localDir())
	if err != nil || after.Tree == before.Tree {
		return before
	}
	r.verbosef("ledger: the work tree changed during the run; no tree identity recorded")
	before.Tree, before.Dirty = "", after.Dirty
	return before
}

// tag stamps who ran it and on which code onto a ledger record.
func (r *run) tag(rec *history.Record, co gitutil.Checkout) {
	rec.Session, rec.Item, rec.Gate = r.opts.Session, r.opts.Item, r.opts.Gate
	rec.Repo, rec.Worktree, rec.Subdir = co.Repo, co.Worktree, co.Subdir
	rec.Commit, rec.Tree, rec.Dirty = co.Commit, co.Tree, co.Dirty
}
