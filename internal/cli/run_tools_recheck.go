package cli

import (
	"time"

	"github.com/LysCongThanh/heavyq/internal/config"
	"github.com/LysCongThanh/heavyq/internal/toolcheck"
)

// Placement by tools (run_tools_placement.go) is decided before a ticket is
// queued, and the ticket keeps that verdict while it waits, except where what
// it was about changes: a move to other machines (heavyq retarget) is placed by
// tools as a run is, and an edit of the env_path or transport of a remote the
// ticket may run on asks again, so a waiting ticket never trusts what the PATH
// a job used to get there had.

// toolsLacking asks the remotes of p's target (the remotes it may run on, before
// any is left out for a tool) whether the run's tools resolve there, and returns
// those that lack one. Only remotes the metrics probe reached are asked; the
// others answer from the cache or not at all (unknown is not missing). When no
// machine would be left and a cached "missing" is behind that, those remotes
// are asked again: the tool may have been installed meanwhile.
func (r *run) toolsLacking(p *placement) map[string][]string {
	all := *p
	all.lacking = nil
	remotes := remoteCandidates(all.machines())
	tools := toolcheck.Union(argvTools(r.opts.Argv), r.opts.Tools)
	if len(remotes) == 0 || len(tools) == 0 {
		return nil
	}
	reached := func(n string) bool { _, ok := r.probes.get(n); return ok }
	asked := time.Now()
	answers, _ := remoteToolAnswers(r.cfg, remotes, tools, reached, false)
	all.lacking = missingTools(answers, tools)
	if len(all.machines()) == 0 && answeredBefore(answers, all.lacking, asked) {
		answers, _ = remoteToolAnswers(r.cfg, remotes, tools, reached, true)
		all.lacking = missingTools(answers, tools)
	}
	return all.lacking
}

// recheckToolsAfterEdit asks again when the edit just adopted (old was the
// config before it) changed what a tool answer of one of the ticket's remotes
// is about: its env_path or its transport. A remote that now has the tools is
// a candidate again, one that lost one is left out, and a ticket no machine is
// left for gives up with exit 2, as it would have been refused at submit.
func (r *run) recheckToolsAfterEdit(old *config.Config) int {
	if r.pl == nil || r.me == nil { // not placed yet: nothing was decided by tools
		return 0
	}
	all := *r.pl
	all.lacking = nil
	if !toolKeysChanged(old, r.cfg, remoteCandidates(all.machines())) {
		return 0
	}
	all.lacking = r.toolsLacking(r.pl)
	if cands := all.machines(); len(cands) == 0 && len(all.lacking) > 0 {
		r.giveUp("the config changed while waiting and no machine of %s has the tools it needs: %s on the PATH its jobs get; giving up",
			r.pl.target, lacksSummary(r.cfg, all.lacking))
		return ExitUsage
	}
	r.pl.lacking, r.pl.candidates = all.lacking, all.machines()
	r.me.Candidates, r.me.Lacks = r.pl.candidates, r.pl.lacking
	r.noteLacking(r.pl.lacking)
	return 0
}

// toolKeysChanged reports whether any remote of names has another env_path or
// transport in next than in old (toolcheck.Key: what a tool answer is about).
// A remote next does not have is not a change here: it is down for the ticket.
func toolKeysChanged(old, next *config.Config, names []string) bool {
	for _, n := range names {
		m, ok := next.Machine(n)
		if !ok {
			continue
		}
		o, had := old.Machine(n)
		if !had || toolcheck.Key(o.EnvPath, o.Transport) != toolcheck.Key(m.EnvPath, m.Transport) {
			return true
		}
	}
	return false
}
