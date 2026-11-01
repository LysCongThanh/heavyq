package config

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// HookConfig is the [hook] section: how `heavyq hook claude-pretooluse`
// rewrites agent commands.
type HookConfig struct {
	// Enabled turns the hook off when false (it then never rewrites).
	Enabled *bool `toml:"enabled"`
	// DefaultRules appends the built-in rules after the user's (default true).
	DefaultRules *bool `toml:"default_rules"`
	// Permission is "" (the rewritten command goes through Claude Code's
	// normal permission flow) or "allow" (auto-approve rewritten commands).
	Permission string `toml:"permission"`
	// MaxWait is passed as --max-wait and added to the Bash tool timeout.
	MaxWait Duration `toml:"max_wait"`
	// Binary is how the rewritten command calls heavyq ("" = "heavyq" when it
	// is on PATH, else the absolute path of the running binary).
	Binary string     `toml:"binary"`
	Rules  []HookRule `toml:"rule"`
}

// HookRule wraps commands whose words start with one of Match.
type HookRule struct {
	Match    []string `toml:"match"`    // word prefixes, e.g. "npm run test"
	Priority int      `toml:"priority"` // 1-9, default 4
	Light    bool     `toml:"light"`
	Lane     string   `toml:"lane"`  // fixed lane ("" = $HEAVYQ_LANE, else worktree name)
	Cache    bool     `toml:"cache"` // add --cache
}

// CacheConfig is the [cache] section for --cache result reuse.
type CacheConfig struct {
	TTL           Duration `toml:"ttl"`
	MaxSizeMB     int      `toml:"max_size_mb"`
	MaxEntryMB    int      `toml:"max_entry_mb"`
	CacheFailures bool     `toml:"cache_failures"`
	// Env lists variable names whose values are part of the cache key.
	Env []string `toml:"env"`
}

const (
	defaultHookMaxWait  = 5 * time.Minute
	defaultHookPrio     = 4
	defaultCacheTTL     = 24 * time.Hour
	defaultCacheSizeMB  = 512
	defaultCacheEntryMB = 64
)

var (
	envNamePattern   = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	matchWordPattern = regexp.MustCompile(`^[A-Za-z0-9_./:@%+=,-]+$`)
)

// HookEnabled reports whether the hook may rewrite commands (default true).
func (c *Config) HookEnabled() bool { return c.Hook.Enabled == nil || *c.Hook.Enabled }

// HookDefaultRules reports whether built-in rules apply (default true).
func (c *Config) HookDefaultRules() bool { return c.Hook.DefaultRules == nil || *c.Hook.DefaultRules }

func (h *HookConfig) normalize() error {
	setDur(&h.MaxWait, defaultHookMaxWait)
	if h.Permission != "" && h.Permission != "allow" {
		return fmt.Errorf("hook.permission %q must be \"\" or \"allow\"", h.Permission)
	}
	if strings.ContainsAny(h.Binary, "'\"`$\\;&|<>()\n") {
		return fmt.Errorf("hook.binary %q contains shell metacharacters", h.Binary)
	}
	for i := range h.Rules {
		r := &h.Rules[i]
		if err := r.Normalize(); err != nil {
			return fmt.Errorf("[[hook.rule]] #%d: %w", i+1, err)
		}
	}
	return nil
}

// Normalize fills defaults and validates one rule.
func (r *HookRule) Normalize() error {
	if len(r.Match) == 0 {
		return errors.New("match must list at least one command prefix")
	}
	for _, m := range r.Match {
		words := strings.Fields(m)
		if len(words) == 0 {
			return errors.New("empty match pattern")
		}
		for _, w := range words {
			if !matchWordPattern.MatchString(w) {
				return fmt.Errorf("match %q: word %q has characters outside [A-Za-z0-9_./:@%%+=,-]", m, w)
			}
		}
	}
	if r.Priority == 0 {
		r.Priority = defaultHookPrio
	}
	if r.Priority < 1 || r.Priority > 9 {
		return fmt.Errorf("priority %d must be 1-9", r.Priority)
	}
	if r.Lane != "" && !SafeName.MatchString(r.Lane) {
		return fmt.Errorf("lane %q must match %s", r.Lane, SafeName)
	}
	return nil
}

func (c *CacheConfig) normalize() error {
	setDur(&c.TTL, defaultCacheTTL)
	if c.MaxSizeMB < 0 || c.MaxEntryMB < 0 {
		return errors.New("cache.max_size_mb/max_entry_mb must be >= 0")
	}
	if c.MaxSizeMB == 0 {
		c.MaxSizeMB = defaultCacheSizeMB
	}
	if c.MaxEntryMB == 0 {
		c.MaxEntryMB = defaultCacheEntryMB
	}
	if c.MaxEntryMB > c.MaxSizeMB {
		c.MaxEntryMB = c.MaxSizeMB
	}
	for _, e := range c.Env {
		if !envNamePattern.MatchString(e) {
			return fmt.Errorf("cache.env: %q is not a valid variable name", e)
		}
	}
	return nil
}
