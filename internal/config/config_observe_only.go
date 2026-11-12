package config

import (
	"errors"
	"fmt"
)

// splitObserved moves the observe_only remotes out of Remotes into Observed, after validating them. An
// observed machine is only probed by `heavyq fleet` (load, RAM, top processes, foreign slots): it is never
// a run target, so nothing that schedules, places or resolves --remote can see it.
func (c *Config) splitObserved() error {
	kept := c.Remotes[:0:0]
	for i := range c.Remotes {
		r := c.Remotes[i]
		if !r.ObserveOnly {
			kept = append(kept, r)
			continue
		}
		if err := r.normalizeObserved(); err != nil {
			return fmt.Errorf("remote %q (observe_only): %w", r.Name, err)
		}
		c.Observed = append(c.Observed, r)
	}
	c.Remotes = kept
	return nil
}

// normalizeObserved refuses the keys that only mean something for a machine heavyq runs on: setting one on
// an observed machine is a mistake to report, not to ignore. It still needs a way to be reached.
func (m *MachineConfig) normalizeObserved() error {
	for key, set := range map[string]bool{
		"heavy_slots": m.HeavySlots != 0, "light_slots": m.LightSlots != 0, "max_load_ratio": m.MaxLoadRatio != 0,
		"job_mb": m.JobMB != 0, "light_mb": m.LightMB != 0, "reserve_mb": m.ReserveMB != 0,
		"legacy_lock_files": len(m.LegacyLockFiles) > 0, "git_url": m.GitURL != "", "cache_hook": m.CacheHook != "",
		"env_path": m.EnvPath != "", "work_dir": m.WorkDir != "", "allow_untrusted_exit": m.AllowUntrustedExit,
	} {
		if set {
			return fmt.Errorf("%s does not apply: heavyq never runs anything there", key)
		}
	}
	if len(m.Transport) > 0 {
		return nil
	}
	if m.SSH == "" {
		return errors.New("set ssh (an ssh alias) or transport")
	}
	if !SSHAliasPattern.MatchString(m.SSH) {
		return fmt.Errorf("ssh %q must be a plain host alias (put options in ~/.ssh/config)", m.SSH)
	}
	m.Transport = []string{"ssh", "-o", "BatchMode=yes", "-o", "ConnectTimeout=10", m.SSH}
	return nil
}

// FleetMachineNames lists every machine `heavyq fleet` reports on: local, the remotes, then the observed ones.
func (c *Config) FleetMachineNames() []string {
	out := []string{LocalName}
	for _, r := range c.Remotes {
		out = append(out, r.Name)
	}
	for _, r := range c.Observed {
		out = append(out, r.Name)
	}
	return out
}
