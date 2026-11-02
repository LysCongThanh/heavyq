package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/LysCongThanh/heavyq/internal/pools"
	"github.com/LysCongThanh/heavyq/internal/tomlkeys"
)

// SafeName matches names that are safe to embed in remote paths and refs.
var SafeName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

// DefaultPath returns the config path heavyq reads when none is given.
func DefaultPath() string {
	if p := os.Getenv("HEAVYQ_CONFIG"); p != "" {
		return p
	}
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "heavyq", "config.toml")
}

// Load reads the config at path. A missing file is not an error unless
// required is true (an explicit --config / HEAVYQ_CONFIG was given).
func Load(path string, required bool) (*Config, error) {
	if path == "" {
		return defaults()
	}
	data, err := os.ReadFile(path)
	switch {
	case err == nil:
		return parse(path, data)
	case errors.Is(err, os.ErrNotExist) && !required:
		return defaults()
	default:
		return nil, fmt.Errorf("config %s: %w", path, err)
	}
}

// defaults returns the config used when there is no file.
func defaults() (*Config, error) {
	cfg := &Config{}
	if err := cfg.normalize(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// parse decodes, strictly checks and validates data, the content of path.
// Keys must match the schema exactly: the TOML library matches them to fields
// ignoring case, so tomlkeys.Check refuses upper-case keys ("[LOCAL]" next to
// "[local]" would otherwise merge, one silently overriding the other).
func parse(path string, data []byte) (*Config, error) {
	cfg := &Config{}
	md, err := toml.Decode(string(data), cfg)
	if err != nil {
		return nil, fmt.Errorf("config %s: %w", path, err)
	}
	if err := tomlkeys.Check(path, data, md); err != nil {
		return nil, err
	}
	cfg.Path = path
	if err := cfg.normalize(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return cfg, nil
}

// normalize fills defaults and rejects unsafe or nonsensical values.
func (c *Config) normalize() error {
	q := &c.Queue
	setDur(&q.Poll, defaultPoll)
	setDur(&q.ForceAfter, defaultForceAfter)
	setDur(&q.RemoteDownMax, defaultRemoteDownMax)
	setDur(&q.HoldFor, defaultHoldFor)
	setDur(&q.ProbeTimeout, defaultProbeTimeout)
	if q.LoadWeightMB <= 0 {
		q.LoadWeightMB = defaultLoadWeightMB
	}
	if q.BaseRef != "" && !validBaseRef(q.BaseRef) {
		return fmt.Errorf("queue.base_ref %q must look like remote/branch", q.BaseRef)
	}

	c.Local.Name = LocalName
	if c.Local.HeavySlots == 0 && c.Local.LightSlots == 0 && len(c.Local.LegacyLockFiles) == 0 {
		c.Local.HeavySlots, c.Local.LightSlots = 2, 1
	}
	if n := len(c.Local.LegacyLockFiles); n > 0 {
		c.Local.HeavySlots = n
	}
	if err := c.Local.normalizeCommon(); err != nil {
		return fmt.Errorf("[local]: %w", err)
	}
	if err := c.Hook.normalize(); err != nil {
		return err
	}
	if err := c.Cache.normalize(); err != nil {
		return err
	}
	var err error
	if c.Pools, err = pools.Build(c.PoolSpecs); err != nil {
		return err
	}
	seen := map[string]bool{LocalName: true}
	for i := range c.Remotes {
		r := &c.Remotes[i]
		if !SafeName.MatchString(r.Name) || seen[r.Name] {
			return fmt.Errorf("[[remote]] #%d: name %q must be unique, not %q, and match %s", i+1, r.Name, LocalName, SafeName)
		}
		seen[r.Name] = true
		if err := r.normalizeRemote(); err != nil {
			return fmt.Errorf("remote %q: %w", r.Name, err)
		}
	}
	return nil
}

func (m *MachineConfig) normalizeCommon() error {
	if m.HeavySlots < 0 || m.LightSlots < 0 || m.HeavySlots+m.LightSlots > 64 {
		return errors.New("heavy_slots/light_slots must be >= 0 and total <= 64")
	}
	if m.HeavySlots+m.LightSlots == 0 {
		return errors.New("needs at least one slot")
	}
	if m.MaxLoadRatio == 0 {
		m.MaxLoadRatio = defaultLoadRatio
	}
	if m.MaxLoadRatio < 0 || m.MaxLoadRatio > 64 {
		return errors.New("max_load_ratio must be in (0, 64]")
	}
	for _, v := range []*int{&m.JobMB, &m.LightMB, &m.ReserveMB} {
		if *v < 0 {
			return errors.New("job_mb/light_mb/reserve_mb must be >= 0")
		}
	}
	if m.JobMB == 0 {
		m.JobMB = defaultJobMB
	}
	if m.LightMB == 0 {
		m.LightMB = defaultLightMB
	}
	if m.ReserveMB == 0 {
		m.ReserveMB = defaultReserveMB
	}
	return nil
}

func (m *MachineConfig) normalizeRemote() error {
	if m.HeavySlots == 0 && m.LightSlots == 0 {
		m.HeavySlots = 2
	}
	if err := m.normalizeCommon(); err != nil {
		return err
	}
	if len(m.LegacyLockFiles) > 0 {
		return errors.New("legacy_lock_files is only valid in [local]")
	}
	if m.WorkDir == "" {
		m.WorkDir = defaultRemoteWorkDir
	}
	if strings.ContainsAny(m.WorkDir, "'\"`$\\\n ") || strings.Contains(m.WorkDir, "..") {
		return fmt.Errorf("work_dir %q contains unsafe characters", m.WorkDir)
	}
	if len(m.Transport) == 0 {
		if m.SSH == "" {
			return errors.New("set ssh (an ssh alias) or transport")
		}
		if !sshAliasPattern.MatchString(m.SSH) {
			return fmt.Errorf("ssh %q must be a plain host alias (put options in ~/.ssh/config)", m.SSH)
		}
		m.Transport = []string{"ssh", "-o", "BatchMode=yes", "-o", "ConnectTimeout=10", m.SSH}
	}
	if m.GitURL == "" {
		if m.SSH == "" {
			return errors.New("git_url is required when transport is customised without ssh")
		}
		m.GitURL = m.SSH + ":" + strings.TrimSuffix(m.WorkDir, "/") + "/git"
	}
	return nil
}

// GateEnabled reports whether the RAM/load gate is on (default true).
func (c *Config) GateEnabled() bool { return c.Queue.ResourceGate == nil || *c.Queue.ResourceGate }

// AutoEnabled reports whether default tickets may be placed by score.
func (c *Config) AutoEnabled() bool {
	return (c.Queue.Auto == nil || *c.Queue.Auto) && len(c.Remotes) > 0 && os.Getenv("HEAVYQ_AUTO") != "0"
}

// Machine returns the machine config by name.
func (c *Config) Machine(name string) (MachineConfig, bool) {
	if name == LocalName {
		return c.Local, true
	}
	for _, r := range c.Remotes {
		if r.Name == name {
			return r, true
		}
	}
	return MachineConfig{}, false
}
