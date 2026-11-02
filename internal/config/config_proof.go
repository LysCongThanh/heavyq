package config

import "fmt"

// ProofConfig is the [proof] section: where `heavyq item done --run` and
// `heavyq gate run` run proofs (docs/items.md). The same table in the
// repository's heavyq.toml overrides it for that repository.
type ProofConfig struct {
	// Remote is "local" (the default), "auto" (the scheduler picks among
	// local and the configured remotes by free slots, then load) or the name
	// of a [[remote]]. A gate with its own remote key keeps it; --remote wins
	// over everything.
	Remote string `toml:"remote"`
}

// RemoteAuto is the --remote value that lets the scheduler pick a machine.
const RemoteAuto = "auto"

// normalize checks the section against the remotes of the same file.
func (p *ProofConfig) normalize(c *Config) error {
	if p.Remote == "" {
		return nil
	}
	if err := c.CheckRemoteTarget(p.Remote); err != nil {
		return fmt.Errorf("[proof] remote: %w", err)
	}
	return nil
}

// CheckRemoteTarget accepts "local", "auto" or the name of a configured
// [[remote]]; anything else is an error naming what is configured.
func (c *Config) CheckRemoteTarget(v string) error {
	if v == LocalName || v == RemoteAuto {
		return nil
	}
	if _, ok := c.Machine(v); ok {
		return nil
	}
	if len(c.Remotes) == 0 {
		return fmt.Errorf("%q is not %q or %q, and no [[remote]] is configured", v, LocalName, RemoteAuto)
	}
	names := make([]string, len(c.Remotes))
	for i, r := range c.Remotes {
		names[i] = r.Name
	}
	return fmt.Errorf("%q is not %q, %q or a [[remote]] (%v)", v, LocalName, RemoteAuto, names)
}
