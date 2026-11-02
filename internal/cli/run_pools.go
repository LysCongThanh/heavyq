package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/LysCongThanh/heavyq/internal/config"
	"github.com/LysCongThanh/heavyq/internal/pools"
	"github.com/LysCongThanh/heavyq/internal/queue"
	"github.com/LysCongThanh/heavyq/internal/repoconfig"
)

// addNeeds merges one --needs value into o.Needs. The flag may repeat; a pool
// named twice, in one flag or across flags, is refused.
func (o *runOptions) addNeeds(spec string) error {
	ns, err := pools.ParseNeeds(spec)
	if err != nil {
		return fmt.Errorf("--needs: %w", err)
	}
	all := append(slices.Clone(o.Needs), ns...)
	if _, err := pools.ParseNeedList(pools.FormatNeeds(all)); err != nil {
		return fmt.Errorf("--needs: %w", err)
	}
	o.Needs = all
	return nil
}

// poolDir is where the lock files of pool values live.
func poolDir() string { return filepath.Join(QueueDir(), "pools") }

// poolSetFor is the pools a run started in dir can ask for: those of the user
// config, with those of the repository's heavyq.toml on top (same name: the
// repository wins). An invalid heavyq.toml is an error, not a silent skip.
func poolSetFor(cfg *config.Config, dir string) (*pools.Set, error) {
	defs := cfg.Pools
	repo, err := repoconfig.Load(dir)
	if err != nil {
		return nil, err
	}
	if repo != nil {
		defs = pools.Merge(defs, repo.Pools)
	}
	return pools.NewSet(poolDir(), defs), nil
}

// displayPoolSet is the same for status and the dashboard, which only show:
// a heavyq.toml that cannot be read leaves the user's pools.
func displayPoolSet(cfg *config.Config) *pools.Set {
	wd, err := os.Getwd()
	if err == nil {
		if set, err := poolSetFor(cfg, wd); err == nil {
			return set
		}
	}
	return pools.NewSet(poolDir(), cfg.Pools)
}

// preparePools resolves and checks the needs of a run before it queues: an
// unknown pool or a need larger than the pool is a usage error, because
// waiting for it would never end.
func (r *run) preparePools() error {
	if len(r.opts.Needs) == 0 {
		return nil
	}
	dir := r.opts.Repo
	if dir == "" {
		dir = r.pl.wd
	}
	set, err := poolSetFor(r.cfg, dir)
	if err != nil {
		return err
	}
	if err := set.Validate(r.opts.Needs); err != nil {
		return fmt.Errorf("--needs: %w", err)
	}
	r.poolSet = set
	r.me.Needs = pools.FormatNeeds(r.opts.Needs)
	return nil
}

// claimPools takes the pool values of the ticket for the claim c, on top of
// the slot c already holds. It returns "" on success, else the wait reason
// (the caller then gives the slot back: no partial holds). In dry mode it
// only checks.
func claimPools(in evalInput, c *claim) string {
	if len(in.ticket.Needs) == 0 {
		return ""
	}
	if in.poolSet == nil {
		if in.dry {
			return ""
		}
		return "pools not loaded"
	}
	needs, err := pools.ParseNeedList(in.ticket.Needs)
	if err != nil {
		return "" // a ticket file this version cannot read: do not guess
	}
	var short *pools.Shortfall
	if in.dry {
		short = in.poolSet.Check(needs)
	} else {
		lease, sf, err := in.poolSet.Acquire(needs)
		if err != nil {
			return err.Error()
		}
		c.lease, short = lease, sf
	}
	if short == nil {
		return ""
	}
	lanes, n := poolHolders(in.snap, short.Pool)
	return short.Reason(lanes, n)
}

// poolHolders names the lanes of the running tickets that hold values of the
// pool, and how many values they hold together.
func poolHolders(snap queue.Snapshot, pool string) ([]string, int) {
	var lanes []string
	n := 0
	for _, t := range snap.Running {
		if k := len(t.Pools[pool]); k > 0 {
			n += k
			if !slices.Contains(lanes, t.Lane) {
				lanes = append(lanes, t.Lane)
			}
		}
	}
	slices.Sort(lanes) // the order of the tickets is a race; the sentence must not flip
	return lanes, n
}

// skipPoolBlocked removes from ahead the tickets that wait for a pool right
// now and share no pool with me: they cannot start, and nothing of theirs is
// held, so they need not keep me from a free slot. A ticket that shares a pool
// with me still blocks me, so I cannot take the values it is waiting for.
func skipPoolBlocked(set *pools.Set, me *queue.Ticket, ahead []*queue.Ticket) []*queue.Ticket {
	if set == nil {
		return ahead
	}
	mine := poolNames(me.Needs)
	var out []*queue.Ticket
	for _, t := range ahead {
		if len(t.Needs) > 0 && !sharesAny(poolNames(t.Needs), mine) {
			if needs, err := pools.ParseNeedList(t.Needs); err == nil && set.Check(needs) != nil {
				continue
			}
		}
		out = append(out, t)
	}
	return out
}

func poolNames(specs []string) []string {
	out := make([]string, len(specs))
	for i, s := range specs {
		out[i], _, _ = strings.Cut(s, ":")
	}
	return out
}

func sharesAny(a, b []string) bool {
	for _, x := range a {
		if slices.Contains(b, x) {
			return true
		}
	}
	return false
}

// setForAhead is the pool set a round uses to tell which tickets ahead wait
// for a pool: the run's own, or, for a run without needs, the user's pools
// and those of the current repository, looked up only when someone ahead has
// needs.
func (r *run) setForAhead(ahead []*queue.Ticket) *pools.Set {
	if r.poolSet != nil {
		return r.poolSet
	}
	for _, t := range ahead {
		if len(t.Needs) > 0 {
			return displayPoolSet(r.cfg)
		}
	}
	return nil
}
