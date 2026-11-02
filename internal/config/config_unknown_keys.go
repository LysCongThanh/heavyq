package config

import (
	"fmt"
	"strings"
)

// UnknownKey is a key of the user config that this heavyq does not define.
type UnknownKey struct {
	Key  string // dotted, as "proof.remote" or "future"
	Line int    // 1-based; 0 when it could not be found
}

// String reads as "proof.remote (line 7)".
func (u UnknownKey) String() string {
	if u.Line > 0 {
		return fmt.Sprintf("%s (line %d)", u.Key, u.Line)
	}
	return u.Key
}

// UnknownKeysWarning is the one-line warning printed once per run for keys the
// config has and this heavyq does not know, or "" when there are none. The
// likely cause is a config written for a newer heavyq.
func (c *Config) UnknownKeysWarning() string {
	if len(c.Unknown) == 0 {
		return ""
	}
	keys := make([]string, len(c.Unknown))
	for i, u := range c.Unknown {
		keys[i] = u.String()
	}
	return fmt.Sprintf("heavyq: warning: %s has %d key(s) this heavyq does not know, ignored: %s (a newer heavyq may define them; heavyq doctor lists them)",
		c.Path, len(keys), strings.Join(keys, ", "))
}
