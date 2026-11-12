package cli

import (
	"path/filepath"

	"github.com/LysCongThanh/heavyq/internal/config"
)

// The waiting ticket's side of `heavyq retarget`: on its next poll it reads the
// move request, re-plans its machines for the new target and answers the
// requester. retarget_rules.go has the rules both sides apply.

// shipped is what a ticket records for a later move to a remote: the
// repository and the commit a remote run ships, resolved at enqueue when the
// placement may leave this machine (resolveRemoteSource ran). A ticket placed
// on this machine only records nothing, so it can never be moved to a remote.
func (p *placement) shipped() (repo, commit string) {
	if p.target == config.LocalName || p.repo == "" || p.sha == "" {
		return "", ""
	}
	return filepath.Base(p.repo), p.sha
}

// applyRetarget applies a pending move request. It runs under the queue mutex,
// before the round places the ticket, so the new machines count from this round
// on. A move that is not safe for this run changes nothing; the answer says why.
func (r *run) applyRetarget() {
	box := controlBox()
	where, ok := box.TakeTarget(r.me.Name)
	if !ok {
		return
	}
	why := r.retarget(where)
	if why == "" {
		r.note("moved to %s by an operator", where)
	} else {
		r.note("move to %s refused: %s", where, why)
	}
	if err := box.AnswerTarget(r.me.Name, why); err != nil {
		r.verbosef("retarget: answer not written: %v", err)
	}
}

// retarget re-plans the run for target where, as if it had been queued with
// --remote WHERE: a remote ships the commit resolved at enqueue, this machine
// runs the work tree in place, and an implicit placement becomes the operator's
// explicit one. It returns why it cannot ("" when the ticket moved).
func (r *run) retarget(where string) string {
	if err := checkRetarget(r.cfg, r.me, where); err != nil {
		return err.Error()
	}
	next := *r.pl
	next.target, next.defaultMode = where, false
	// The move is the operator's act now: auto means the remotes of the config
	// this ticket follows now (reloaded while it waited), and the ticket keeps
	// them across later edits as it kept the ones it was submitted with.
	next.remotes = remoteNames(r.cfg)
	cands := next.machines() // leaves this machine out of a list or auto while it is not eligible
	if r.pl.localWhy != "" && (where == config.LocalName || len(cands) == 0) {
		return "this machine cannot run it: " + r.pl.localWhy
	}
	target, old := r.me.Target, r.me.Candidates
	r.me.Target, r.me.Candidates = where, cands
	if err := r.q.Rewrite(r.me); err != nil {
		r.me.Target, r.me.Candidates = target, old
		return "ticket not rewritten: " + err.Error()
	}
	next.candidates = cands
	*r.pl = next
	return ""
}
