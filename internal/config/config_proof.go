package config

import "fmt"

// ProofConfig is the [proof] section: where `heavyq item done --run` and
// `heavyq gate run` run proofs (docs/items.md). The same table in the
// repository's heavyq.toml overrides it for that repository.
type ProofConfig struct {
	// Remote is "local" (the default), "auto" (the scheduler picks among
	// local and the configured remotes by free slots, then load), the name
	// of a [[remote]], or an ordered list "a,b,c" (the first machine with a
	// free slot that passes the resource gate). A gate with its own remote
	// key keeps it; --remote wins over everything.
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

// CheckRemoteTarget accepts "local", "auto", the name of a configured
// [[remote]], or a list of those names and "local" (CheckMachineList);
// anything else is an error naming what is configured.
func (c *Config) CheckRemoteTarget(v string) error {
	if v == LocalName || v == RemoteAuto {
		return nil
	}
	if IsMachineList(v) {
		ms, err := CheckMachineList(v)
		if err != nil {
			return err
		}
		for _, m := range ms {
			if err := c.CheckRemoteTarget(m); err != nil {
				return fmt.Errorf("in the list %q: %w", v, err)
			}
		}
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
