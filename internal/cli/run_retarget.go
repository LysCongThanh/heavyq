package cli

import (
	"path/filepath"
	"time"

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

// applyRetarget applies a pending move request, right before the round that
// places the ticket, so the new machines count from that round on. It runs
// outside the queue mutex: placing the move by tools may probe the new remotes,
// and a slow remote must never hold up anyone else's round (the ticket file is
// rewritten atomically). A move that is not safe for this run changes nothing;
// the answer says why.
func (r *run) applyRetarget() {
	box := controlBox()
	where, ok := box.TakeTarget(r.me.Name)
	if !ok {
		return
	}
	why := r.retarget(where)
	if why == "" {
		r.moved = true
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
// explicit one. The ticket keeps its priority and its place (Since), but not
// its anti-starvation clock: that clock measured a wait for the old machines,
// so the new ones start it over. It returns why it cannot ("" when the ticket moved).
func (r *run) retarget(where string) string {
	if err := checkRetarget(r.cfg, r.me, where); err != nil {
		return err.Error()
	}
	next := *r.pl
	next.target, next.defaultMode = where, false
	// The move is the operator's act now: auto means the remotes of the config
	// this ticket follows now (reloaded while it waited), and the ticket keeps
	// them across later edits as it kept the ones it was submitted with.
	next.remotes, next.lacking = remoteNames(r.cfg), nil
	// Placed by tools as a run is (run_tools_recheck.go): a remote of the new
	// target that lacks a tool the run needs is left out, and a move that
	// leaves no machine is refused. Only remotes a probe reaches are asked.
	r.probes.refresh(r.cfg, remoteCandidates(next.machines()))
	next.lacking = r.toolsLacking(&next)
	cands := next.machines() // leaves this machine out of a list or auto while it is not eligible
	if len(cands) == 0 && len(next.lacking) > 0 {
		return "no machine of " + where + " can run it: " + lacksSummary(r.cfg, next.lacking) +
			" on the PATH its jobs get; add the tool's directory to that [[remote]]'s env_path, or choose another --remote"
	}
	if r.pl.localWhy != "" && (where == config.LocalName || len(cands) == 0) {
		return "this machine cannot run it: " + r.pl.localWhy
	}
	target, old, head, lacks := r.me.Target, r.me.Candidates, r.me.HeadSince, r.me.Lacks
	r.me.Target, r.me.Candidates, r.me.HeadSince, r.me.Lacks = where, cands, time.Time{}, next.lacking
	if err := r.q.Rewrite(r.me); err != nil {
		r.me.Target, r.me.Candidates, r.me.HeadSince, r.me.Lacks = target, old, head, lacks
		return "ticket not rewritten: " + err.Error()
	}
	next.candidates = cands
	*r.pl = next
	r.noteLacking(next.lacking)
	return ""
}
