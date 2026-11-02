// Package config loads and validates the heavyq TOML configuration.
//
// Every field has a safe default, so heavyq works with no config file at all
// (local machine only, 2 heavy slots, 1 light slot).
package config

import (
	"time"

	"github.com/LysCongThanh/heavyq/internal/pools"
)

// Duration wraps time.Duration so TOML values like "15m" decode naturally.
type Duration struct{ time.Duration }

// UnmarshalText implements encoding.TextUnmarshaler for TOML strings.
func (d *Duration) UnmarshalText(b []byte) error {
	v, err := time.ParseDuration(string(b))
	if err != nil {
		return err
	}
	d.Duration = v
	return nil
}

// Config is the whole configuration file.
type Config struct {
	Queue   QueueConfig     `toml:"queue"`
	Local   MachineConfig   `toml:"local"`
	Remotes []MachineConfig `toml:"remote"`
	Hook    HookConfig      `toml:"hook"`
	Cache   CacheConfig     `toml:"cache"`
	Mentor  MentorConfig    `toml:"mentor"`
	Proof   ProofConfig     `toml:"proof"`
	// PoolSpecs are the [[pool]] tables as written; normalize validates them
	// into Pools. A pool of the same name in heavyq.toml replaces the user's.
	PoolSpecs []pools.Spec `toml:"pool"`

	// Path is the file the config was loaded from ("" when defaults only).
	Path string `toml:"-"`
	// Pools are the validated [[pool]] tables, in file order.
	Pools []pools.Pool `toml:"-"`
}

// QueueConfig holds queue-wide behaviour.
type QueueConfig struct {
	// Poll is the delay between two scheduling attempts of a waiting ticket.
	Poll Duration `toml:"poll"`
	// ForceAfter: a head-of-line ticket waiting longer than this bypasses the
	// resource gate (anti-starvation). It still needs a free slot.
	ForceAfter Duration `toml:"force_after"`
	// ResourceGate disables RAM/load checks when false (slots only).
	ResourceGate *bool `toml:"resource_gate"`
	// Auto enables score-based placement for tickets without --remote.
	Auto *bool `toml:"auto"`
	// LoadWeightMB is k in score = free_mb - k * load1.
	LoadWeightMB int `toml:"load_weight_mb"`
	// RemoteDownMax: a remote-only ticket exits 76 after its remotes have been
	// unreachable this long.
	RemoteDownMax Duration `toml:"remote_down_max"`
	// HoldFor: a freshly claimed job reserves its RAM budget for this long,
	// because a starting job has not grown yet.
	HoldFor Duration `toml:"hold_for"`
	// ProbeTimeout bounds one remote metrics probe.
	ProbeTimeout Duration `toml:"probe_timeout"`
	// BaseRef is the default --base-ref ("" = none).
	BaseRef string `toml:"base_ref"`
}

// MachineConfig describes the local machine or one remote.
type MachineConfig struct {
	Name         string  `toml:"name"`
	HeavySlots   int     `toml:"heavy_slots"`
	LightSlots   int     `toml:"light_slots"`
	MaxLoadRatio float64 `toml:"max_load_ratio"`
	JobMB        int     `toml:"job_mb"`
	LightMB      int     `toml:"light_mb"`
	ReserveMB    int     `toml:"reserve_mb"`

	// Local only: lock files shared with legacy `flock <file> cmd` users.
	// When set they replace the generated heavy slot files.
	LegacyLockFiles []string `toml:"legacy_lock_files"`

	// Remote only.
	SSH       string   `toml:"ssh"`        // ssh alias / host
	Transport []string `toml:"transport"`  // argv prefix; the remote script is appended as ONE argument
	WorkDir   string   `toml:"work_dir"`   // remote dir holding git/, wt/, slots/, cache/
	GitURL    string   `toml:"git_url"`    // where bare repos live, as seen by `git push`
	CacheHook string   `toml:"cache_hook"` // shell snippet run in the worktree before the command
	EnvPath   string   `toml:"env_path"`   // directories put first on the remote command's PATH ("~/" = remote home)
}

// IsLocal reports whether the machine is the local workstation.
func (m MachineConfig) IsLocal() bool { return m.Name == LocalName }

// LocalName is the reserved machine name for the local workstation.
const LocalName = "local"

// Default values.
const (
	defaultPoll          = 2 * time.Second
	defaultForceAfter    = 15 * time.Minute
	defaultRemoteDownMax = 10 * time.Minute
	defaultHoldFor       = time.Minute
	defaultProbeTimeout  = 5 * time.Second
	defaultLoadWeightMB  = 500
	defaultLoadRatio     = 0.75
	defaultJobMB         = 3072
	defaultLightMB       = 1024
	defaultReserveMB     = 2048
	defaultRemoteWorkDir = ".heavyq"
)
