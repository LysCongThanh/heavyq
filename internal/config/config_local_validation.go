package config

import "errors"

// Default slot counts of [local], each applied when its own key is absent.
const (
	defaultLocalHeavySlots = 2
	defaultLocalLightSlots = 1
)

// slotKeys records which slot keys a table sets: an absent key gets its own
// default, while an explicit 0 is kept (a light-only machine is allowed).
type slotKeys struct{ heavy, light bool }

// normalizeLocal fills the [local] defaults and refuses what [local] cannot use.
func (c *Config) normalizeLocal() error {
	m := &c.Local
	m.Name = LocalName
	if n := len(m.LegacyLockFiles); n > 0 {
		// The files ARE the heavy slots; light_slots stays as written (0 when absent).
		m.HeavySlots = n
	} else {
		if !c.localSlotKeys.heavy {
			m.HeavySlots = defaultLocalHeavySlots
		}
		if !c.localSlotKeys.light {
			m.LightSlots = defaultLocalLightSlots
		}
	}
	return m.normalizeCommon()
}

// checkSomeHeavySlot: runs, gates and hook rewrites are heavy by default, and
// a heavy ticket can only take a heavy slot, so a config where no machine has
// one would let them wait forever.
func (c *Config) checkSomeHeavySlot() error {
	n := c.Local.HeavySlots
	for _, r := range c.Remotes {
		n += r.HeavySlots
	}
	if n == 0 {
		return errors.New("no machine has heavy_slots > 0, so a heavy run (the default) could never start")
	}
	return nil
}
