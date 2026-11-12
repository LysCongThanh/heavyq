package cli

import (
	"sync"
	"time"

	"github.com/LysCongThanh/heavyq/internal/config"
	"github.com/LysCongThanh/heavyq/internal/metrics"
	"github.com/LysCongThanh/heavyq/internal/procinfo"
	"github.com/LysCongThanh/heavyq/internal/queue"
	"github.com/LysCongThanh/heavyq/internal/remote"
	"github.com/LysCongThanh/heavyq/internal/schedule"
)

const (
	probeTTL     = 8 * time.Second
	probeFailTTL = 30 * time.Second
)

// probeCache remembers remote probes so a slow or dead remote is contacted
// at most every few seconds, and never while the queue mutex is held.
type probeCache struct {
	mu      sync.Mutex
	timeout time.Duration
	entries map[string]probeEntry
}

type probeEntry struct {
	res remote.ProbeResult
	err error
	at  time.Time
}

func newProbeCache(timeout time.Duration) *probeCache {
	return &probeCache{timeout: timeout, entries: map[string]probeEntry{}}
}

// setTimeout changes how long a probe may take, for the probes started from
// now on (a waiting ticket adopting an edited [queue].probe_timeout).
func (c *probeCache) setTimeout(d time.Duration) {
	c.mu.Lock()
	c.timeout = d
	c.mu.Unlock()
}

// refresh probes the given remotes whose cache entry expired, in parallel. A
// name cfg does not have (a remote an edit removed while a ticket waits) loses
// its entry: it is not reachable through this config, whatever it last answered.
func (c *probeCache) refresh(cfg *config.Config, names []string) {
	var wg sync.WaitGroup
	c.mu.Lock()
	timeout := c.timeout
	c.mu.Unlock()
	for _, n := range names {
		m, ok := cfg.Machine(n)
		if !ok {
			c.mu.Lock()
			delete(c.entries, n)
			c.mu.Unlock()
			continue
		}
		if m.IsLocal() {
			continue
		}
		c.mu.Lock()
		e, seen := c.entries[n]
		c.mu.Unlock()
		ttl := probeTTL
		if e.err != nil {
			ttl = probeFailTTL
		}
		if seen && time.Since(e.at) < ttl {
			continue
		}
		wg.Add(1)
		go func(m config.MachineConfig) {
			defer wg.Done()
			res, err := remote.Probe(m, timeout)
			c.mu.Lock()
			c.entries[m.Name] = probeEntry{res: res, err: err, at: time.Now()}
			c.mu.Unlock()
		}(m)
	}
	wg.Wait()
}

func (c *probeCache) get(name string) (remote.ProbeResult, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[name]
	return e.res, ok && e.err == nil
}

// machineView is a machine's state as seen by one scheduling round.
type machineView struct {
	state schedule.MachineState
	busy  map[string]bool // remote: slot names busy remotely or claimed by our running tickets
}

// buildView assembles the state of machine name from metrics, the probe
// cache and the running tickets of snapshot s.
func buildView(cfg *config.Config, name string, s queue.Snapshot, probes *probeCache, hold time.Duration) machineView {
	m, _ := cfg.Machine(name)
	v := machineView{state: schedule.MachineState{Cfg: m}, busy: map[string]bool{}}
	if m.IsLocal() {
		v.state.Metrics = metrics.Host(procinfo.Default.Root)
		v.state.Reachable = true
	} else if res, ok := probes.get(name); ok {
		v.state.Metrics, v.state.Reachable = res.Metrics, true
		v.state.BusySlots = len(res.Busy)
		for s := range res.Busy {
			v.busy[s] = true
		}
	}
	for _, t := range s.Running {
		if t.Machine != name {
			continue
		}
		v.state.Running++
		if t.Slot != "" {
			v.busy[t.Slot] = true
		}
		if time.Since(t.ClaimedAt) < hold {
			if t.Light {
				v.state.ReservedMB += m.LightMB
			} else {
				v.state.ReservedMB += m.JobMB
			}
		}
	}
	return v
}

func slotCounts(cfg *config.Config) schedule.SlotCounts {
	return func(name string) (int, int) {
		m, ok := cfg.Machine(name)
		if !ok {
			return 0, 0
		}
		return m.HeavySlots, m.LightSlots
	}
}
