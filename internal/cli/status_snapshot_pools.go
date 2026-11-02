package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/LysCongThanh/heavyq/internal/pools"
	"github.com/LysCongThanh/heavyq/internal/queue"
	"github.com/LysCongThanh/heavyq/internal/snapshot"
)

// poolSnapshots lists every pool of set with the values the running tickets
// hold. Pools come in definition order, held values in the pool's own order.
func poolSnapshots(set *pools.Set, qs queue.Snapshot) []snapshot.Pool {
	out := []snapshot.Pool{}
	for _, name := range set.Names() {
		p, _ := set.Pool(name)
		held := map[string]snapshot.PoolHolder{}
		for _, t := range qs.Running {
			for _, v := range t.Pools[name] {
				held[v] = snapshot.PoolHolder{Value: v, Lane: t.Lane, Ticket: t.Name}
			}
		}
		sp := snapshot.Pool{Name: name, Capacity: set.Capacity(name), Holders: []snapshot.PoolHolder{}}
		for _, v := range p.Values {
			if h, ok := held[v]; ok {
				sp.Holders = append(sp.Holders, h)
			}
		}
		sp.Held = len(sp.Holders)
		out = append(out, sp)
	}
	return out
}

// printPools writes the POOLS section of `heavyq status`; nothing when no
// pool is defined.
func printPools(w io.Writer, set *pools.Set, qs queue.Snapshot) {
	ps := poolSnapshots(set, qs)
	if len(ps) == 0 {
		return
	}
	fmt.Fprintln(w, "POOLS")
	for _, p := range ps {
		line := fmt.Sprintf("  %-10s %d/%d held", p.Name, p.Held, p.Capacity)
		var by []string
		for _, h := range p.Holders {
			by = append(by, h.Lane+" ("+h.Value+")")
		}
		if len(by) > 0 {
			line += ": " + strings.Join(by, " · ")
		}
		fmt.Fprintln(w, line)
	}
}

// poolReasonIn reports whether any wait reason of a ticket comes from a pool.
func poolReasonIn(reasons map[string]string) bool {
	for _, r := range reasons {
		if pools.IsReason(r) {
			return true
		}
	}
	return false
}
