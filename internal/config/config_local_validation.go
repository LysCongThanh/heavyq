package config

import (
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
)

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
	if err := m.refuseRemoteOnlyKeys(); err != nil {
		return err
	}
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
	if err := m.normalizeCommon(); err != nil {
		return err
	}
	return m.checkLegacyLockFiles()
}

// refuseRemoteOnlyKeys: [local] decodes into the shared machine type, so the
// remote keys are known there: without this they load with no warning and do
// nothing (local runs never get that env_path or cache_hook).
func (m *MachineConfig) refuseRemoteOnlyKeys() error {
	for _, k := range []struct {
		key string
		set bool
	}{
		{"name", m.Name != "" && m.Name != LocalName},
		{"ssh", m.SSH != ""},
		{"transport", len(m.Transport) > 0},
		{"work_dir", m.WorkDir != ""},
		{"git_url", m.GitURL != ""},
		{"cache_hook", m.CacheHook != ""},
		{"env_path", m.EnvPath != ""},
	} {
		if k.set {
			return fmt.Errorf("%s is only valid in [[remote]]", k.key)
		}
	}
	return nil
}

// checkLegacyLockFiles: every heavyq process opens these paths as written, so a
// relative one is a different file per working directory (no shared limit, no
// exclusion with flock users), "" fails every claim, and a repeated path or
// slot name (the base name without ".lock", which status and claims use) is
// one slot counted twice.
func (m *MachineConfig) checkLegacyLockFiles() error {
	paths, names := map[string]bool{}, map[string]bool{}
	for i := 1; i <= m.LightSlots; i++ {
		names["light-"+strconv.Itoa(i)] = true
	}
	for i, f := range m.LegacyLockFiles {
		key := fmt.Sprintf("legacy_lock_files[%d] %q", i, f)
		if !filepath.IsAbs(f) {
			return errors.New(key + " must be an absolute path")
		}
		clean := filepath.Clean(f)
		name := strings.TrimSuffix(filepath.Base(clean), ".lock")
		switch {
		case paths[clean]:
			return errors.New(key + " is listed twice")
		case names[name]:
			return fmt.Errorf("%s has the slot name %q of another slot; give each file its own base name", key, name)
		}
		paths[clean], names[name] = true, true
		m.LegacyLockFiles[i] = clean
	}
	return nil
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
