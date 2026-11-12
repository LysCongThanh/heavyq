package cli

import (
	"fmt"
	"strings"
	"time"

	"github.com/LysCongThanh/heavyq/internal/config"
	"github.com/LysCongThanh/heavyq/internal/gitutil"
	"github.com/LysCongThanh/heavyq/internal/procinfo"
	"github.com/LysCongThanh/heavyq/internal/queue"
	"github.com/LysCongThanh/heavyq/internal/schedule"
)

const (
	progressEvery = 30 * time.Second
	downWarnEvery = time.Minute
)

// waitForTurn loops until the ticket claims a slot (returns it) or must give
// up (returns nil and the exit code).
func (r *run) waitForTurn() (*claim, int) {
	var headSince, downSince, lastDownWarn time.Time
	for {
		if controlBox().CancelRequested(r.me.Name) {
			r.giveUp("cancelled by an operator while waiting")
			r.cancelled = true
			return nil, ExitCancelled
		}
		if rc := r.reloadConfig(); rc != 0 {
			return nil, rc
		}
		r.probes.refresh(r.cfg, r.me.Candidates)
		if len(r.pl.siblings) > 0 || !r.pl.refIsHead {
			r.pl.refreshLocal()
			if r.pl.target == config.LocalName && r.pl.localWhy != "" {
				r.giveUp("code changed while waiting (%s); giving up", r.pl.localWhy)
				return nil, ExitUsage
			}
			r.me.Candidates = r.pl.machines(r.cfg)
		}
		c, pos, total, ahead, err := r.round(&headSince)
		if err != nil {
			r.giveUp("%v", err)
			return nil, ExitUsage
		}
		if c != nil {
			return c, 0
		}
		now := time.Now()
		r.out.nar.observe(now, waitView{pos: pos, total: total, target: r.pl.target, prio: r.me.Prio,
			lane: r.me.Lane, ahead: ahead})
		if rc := r.checkRemoteDown(&downSince, &lastDownWarn); rc != 0 {
			return nil, rc
		}
		if r.opts.MaxWait > 0 && now.Sub(r.t0) >= r.opts.MaxWait {
			r.giveUp("not our turn after %s (--max-wait)", fmtDur(r.opts.MaxWait))
			return nil, ExitMaxWait
		}
		if s := r.pollOrSignal(r.cfg.Queue.Poll.Duration); s != nil {
			return nil, signalExit(s)
		}
	}
}

// round runs one scheduling decision under the queue mutex.
func (r *run) round(headSince *time.Time) (*claim, int, int, []*queue.Ticket, error) {
	unlock, err := r.q.Mutex()
	if err != nil {
		return nil, 0, 0, nil, err
	}
	defer unlock()
	r.q.Prune(procinfo.Default.Alive)
	if p, ok := controlBox().TakePrio(r.me.Name); ok && p != r.me.Prio {
		if err := r.q.Reprio(r.me, p); err != nil {
			fmt.Fprintln(r.stderr, "heavyq: warning: priority not changed:", err)
		} else {
			fmt.Fprintf(r.stderr, "heavyq: priority changed to %d by an operator\n", p)
		}
	}
	snap, err := r.q.List()
	if err != nil {
		return nil, 0, 0, nil, err
	}
	ahead, pos := schedule.Ahead(snap.Waiting, r.me)
	if pos == 0 {
		return nil, 0, 0, nil, fmt.Errorf("our ticket disappeared from %s", r.q.Path)
	}
	opps, _, head := admissionOpportunities(r.cfg, r.me, ahead, r.setForAhead(ahead))
	was := *headSince
	if !head {
		*headSince = time.Time{}
	} else if headSince.IsZero() {
		*headSince = time.Now()
	}
	// Publish the anti-starvation clock and the config this round decides
	// with, for status / why / dash.
	if !was.Equal(*headSince) || r.me.ConfigSum != r.cfg.Origin.Sum {
		r.me.HeadSince, r.me.ConfigSum = *headSince, r.cfg.Origin.Sum
		_ = r.q.Rewrite(r.me)
	}
	var waited time.Duration
	if head {
		waited = time.Since(*headSince)
	}
	c, _ := evaluate(evalInput{cfg: r.cfg, ticket: r.me, snap: snap, probes: r.probes,
		opps: opps, headWait: waited, needsRAM: r.opts.NeedsRAM, poolSet: r.poolSet})
	if c != nil && c.machine != config.LocalName && r.pl.defaultMode && !r.stillSafeRemotely() {
		c = nil // the repo changed while waiting: no remote this round
	}
	if c == nil {
		return nil, pos, len(snap.Waiting), ahead, nil
	}
	r.me.Forced = c.forced
	r.me.Pools = c.lease.Values()
	if err := r.q.Claim(r.me, c.machine, c.slot); err != nil {
		c.releaseAll()
		return nil, 0, 0, nil, err
	}
	return c, pos, len(snap.Waiting), ahead, nil
}

// stillSafeRemotely re-verifies an implicit auto ticket right before it goes
// remote, and ships the commit that is checked out now.
func (r *run) stillSafeRemotely() bool {
	repo, why := autoEligible(r.pl.wd, &r.opts, r.pl.siblings)
	if why != "" || repo != r.pl.repo {
		return false
	}
	sha, err := gitutil.ResolveCommit(repo, "HEAD")
	if err != nil {
		return false
	}
	r.pl.sha = sha
	return true
}

// checkRemoteDown enforces remote_down_max for tickets that can only run on
// remotes, all of which are unreachable.
func (r *run) checkRemoteDown(since, lastWarn *time.Time) int {
	rem := remoteCandidates(r.me.Candidates)
	if len(rem) == 0 || len(rem) != len(r.me.Candidates) {
		*since = time.Time{}
		return 0
	}
	for _, n := range rem {
		if _, ok := r.probes.get(n); ok {
			*since = time.Time{}
			return 0
		}
	}
	now := time.Now()
	if since.IsZero() {
		*since = now
	}
	down := now.Sub(*since)
	if down >= r.cfg.Queue.RemoteDownMax.Duration {
		r.giveUp("%s unreachable for %s; giving up", strings.Join(rem, ", "), fmtDur(down))
		return ExitRemoteDown
	}
	if !r.out.mode.Terse() && now.Sub(*lastWarn) >= downWarnEvery { // an agent hears it in the digest
		fmt.Fprintf(r.stderr, "heavyq: %s unreachable (%s); exit %d after %s\n", strings.Join(rem, ", "),
			fmtDur(down), ExitRemoteDown, fmtDur(r.cfg.Queue.RemoteDownMax.Duration))
		*lastWarn = now
	}
	return 0
}

func describeAhead(ahead []*queue.Ticket) string {
	if len(ahead) == 0 {
		return ""
	}
	parts := make([]string, 0, len(ahead))
	for _, t := range ahead {
		parts = append(parts, fmt.Sprintf("%s(p%d)", t.Lane, t.Prio))
	}
	return "; ahead: " + strings.Join(parts, " ")
}
