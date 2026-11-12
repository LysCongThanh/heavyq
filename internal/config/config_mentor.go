package config

import "fmt"

// MentorConfig is the [mentor] section: the experiment behind
// `heavyq hook claude-posttooluse` (docs/mentor.md).
type MentorConfig struct {
	// HoldoutPct is the share of eligible events logged but not shown, 0-100
	// (default 20). 100 is shadow mode: events are logged and measured, the
	// agent sees nothing.
	HoldoutPct *int `toml:"holdout_pct"`
	// AutoMute silences a rule that shows too little lift (default true).
	AutoMute *bool `toml:"auto_mute"`
	// MinEvents is how many resolved events each arm needs before the lift
	// is judged (default 200).
	MinEvents int `toml:"min_events"`
	// MinLiftPP is the lift, in percentage points, below which a rule is
	// muted (default 10).
	MinLiftPP *float64 `toml:"min_lift_pp"`
}

const (
	defaultMentorHoldoutPct = 20
	defaultMentorMinEvents  = 200
	defaultMentorMinLiftPP  = 10.0
)

// MentorHoldoutPct is the holdout share in percent (default 20).
func (c *Config) MentorHoldoutPct() int {
	if c.Mentor.HoldoutPct == nil {
		return defaultMentorHoldoutPct
	}
	return *c.Mentor.HoldoutPct
}

// MentorAutoMute reports whether a rule without lift mutes itself (default true).
func (c *Config) MentorAutoMute() bool { return c.Mentor.AutoMute == nil || *c.Mentor.AutoMute }

// MentorMinEvents is the resolved events per arm needed to judge a rule (default 200).
func (c *Config) MentorMinEvents() int {
	if c.Mentor.MinEvents == 0 {
		return defaultMentorMinEvents
	}
	return c.Mentor.MinEvents
}

// MentorMinLiftPP is the lift below which a rule is muted (default 10 points).
func (c *Config) MentorMinLiftPP() float64 {
	if c.Mentor.MinLiftPP == nil {
		return defaultMentorMinLiftPP
	}
	return *c.Mentor.MinLiftPP
}

func (m *MentorConfig) normalize() error {
	if m.HoldoutPct != nil && (*m.HoldoutPct < 0 || *m.HoldoutPct > 100) {
		return fmt.Errorf("mentor.holdout_pct %d must be in [0, 100]", *m.HoldoutPct)
	}
	if m.MinEvents < 0 {
		return fmt.Errorf("mentor.min_events %d must be >= 0", m.MinEvents)
	}
	// "Not inside" also refuses nan, which every comparison lets through.
	if m.MinLiftPP != nil && !(*m.MinLiftPP >= 0 && *m.MinLiftPP <= 100) {
		return fmt.Errorf("mentor.min_lift_pp %v must be in [0, 100]", *m.MinLiftPP)
	}
	return nil
}
