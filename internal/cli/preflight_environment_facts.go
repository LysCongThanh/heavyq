package cli

import (
	"sync"
	"syscall"
	"time"

	"github.com/LysCongThanh/heavyq/internal/config"
	"github.com/LysCongThanh/heavyq/internal/preflight"
	"github.com/LysCongThanh/heavyq/internal/remote"
)

// preflightProbeTimeout bounds one remote probe: preflight is interactive, a
// remote that needs longer than this to answer is not one to leave a night to.
const preflightProbeTimeout = 3 * time.Second

// environmentFacts measures what CheckEnvironment needs: free disk on the file
// system of root, the configured slots, every remote probed in parallel with a
// short timeout, and the pools. Nothing here fails the command: what cannot be
// measured is left unknown.
func environmentFacts(cfg *config.Config, root string, parallel int) preflight.EnvInput {
	in := preflight.EnvInput{Parallel: parallel, DiskPath: root}
	var st syscall.Statfs_t
	if err := syscall.Statfs(root, &st); err == nil {
		in.DiskKnown = true
		in.DiskFreeBytes = uint64(st.Bavail) * uint64(st.Bsize) //nolint:gosec // block counts are not negative
	}
	in.Machines = append(in.Machines, preflight.MachineFact{Name: config.LocalName, Local: true,
		Reachable: true, HeavySlots: len(localSlotPaths(cfg, false))})
	timeout := cfg.Queue.ProbeTimeout.Duration
	if timeout <= 0 || timeout > preflightProbeTimeout {
		timeout = preflightProbeTimeout
	}
	remotes := make([]preflight.MachineFact, len(cfg.Remotes))
	var wg sync.WaitGroup
	for i, m := range cfg.Remotes {
		wg.Add(1)
		go func() {
			defer wg.Done()
			remotes[i] = preflight.MachineFact{Name: m.Name, SSH: m.SSH, HeavySlots: m.HeavySlots, Reachable: true}
			if _, err := remote.Probe(m, timeout); err != nil {
				remotes[i].Reachable, remotes[i].Err = false, oneLine(err.Error(), 80)
			}
		}()
	}
	wg.Wait()
	in.Machines = append(in.Machines, remotes...)
	set := displayPoolSet(cfg)
	for _, name := range set.Names() {
		in.Pools = append(in.Pools, preflight.PoolFact{Name: name, Capacity: set.Capacity(name)})
	}
	return in
}
