package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"

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
// required is true (an explicit --config / HEAVYQ_CONFIG was given). The
// result remembers where it came from (Origin), so a process that waits can
// re-read the same file (Watch).
func Load(path string, required bool) (*Config, error) {
	cfg, err := read(path, required)
	if err != nil {
		return nil, err
	}
	cfg.Origin.Path, cfg.Origin.Required = absPath(path), required
	return cfg, nil
}

// read is Load without the origin.
func read(path string, required bool) (*Config, error) {
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
	cfg := &Config{Origin: Origin{Sum: noFileSum}}
	if err := cfg.normalize(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// parse decodes, checks and validates data, the content of path.
// Keys the schema does not define are kept in Config.Unknown and ignored, so a
// newer config does not brick an older binary (repository heavyq.toml files
// stay strict: CI catches their typos). The TOML library matches keys to
// fields ignoring case, so an upper-case key that does decode is still refused
// ("[LOCAL]" next to "[local]" would otherwise merge, one silently overriding
// the other).
func parse(path string, data []byte) (*Config, error) {
	cfg := &Config{}
	md, err := toml.Decode(string(data), cfg)
	if err != nil {
		return nil, fmt.Errorf("config %s: %w", path, err)
	}
	if key := tomlkeys.FirstMiscased(md); key != nil {
		return nil, tomlkeys.Error(path, data, key)
	}
	for _, key := range tomlkeys.Unknown(md) {
		cfg.Unknown = append(cfg.Unknown, UnknownKey{Key: key.String(), Line: tomlkeys.Line(data, key)})
	}
	cfg.Path, cfg.Origin.Sum = path, contentSum(data)
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
	if q.BaseRef != "" && q.BaseRef != BaseRefNone && !validBaseRef(q.BaseRef) {
		return fmt.Errorf("queue.base_ref %q must look like remote/branch (e.g. origin/main) or %q", q.BaseRef, BaseRefNone)
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
	if err := c.Mentor.normalize(); err != nil {
		return err
	}
	var err error
	if c.Pools, err = pools.Build(c.PoolSpecs); err != nil {
		return err
	}
	if c.Local.ObserveOnly {
		return errors.New("[local]: observe_only is for a [[remote]]")
	}
	seen := map[string]bool{LocalName: true}
	for i := range c.Remotes {
		r := &c.Remotes[i]
		if !SafeName.MatchString(r.Name) || seen[r.Name] {
			return fmt.Errorf("[[remote]] #%d: name %q must be unique, not %q, and match %s", i+1, r.Name, LocalName, SafeName)
		}
		seen[r.Name] = true
		if r.ObserveOnly {
			continue // validated by splitObserved
		}
		if err := r.normalizeRemote(); err != nil {
			return fmt.Errorf("remote %q: %w", r.Name, err)
		}
	}
	if err := c.splitObserved(); err != nil {
		return err
	}
	if err := c.Run.normalize(); err != nil {
		return err
	}
	if err := c.Proof.normalize(c); err != nil {
		return err
	}
	return c.normalizeForeign()
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
