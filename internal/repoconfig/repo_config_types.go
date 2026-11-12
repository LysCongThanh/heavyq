// Package repoconfig loads the per-repository file heavyq.toml, which lives
// at the git root and says what "proven" means for that repository: named
// gates, each a list of shell commands.
//
// Trust model: heavyq.toml is trusted like a Makefile. Its commands are run
// only by an explicit `heavyq gate run`, never by a hook. Everything in it is
// still validated and unknown keys are refused, so a typo cannot silently
// weaken a gate.
package repoconfig

import "github.com/LysCongThanh/heavyq/internal/pools"

// FileName is the repo config file at the git root.
const FileName = "heavyq.toml"

// Limits and defaults of a [[gate]].
const (
	DefaultPrio   = 3
	MinPrio       = 1
	MaxPrio       = 9
	MaxRunLen     = 2000 // bytes per command string
	DefaultRemote = "local"
	// maxFileBytes bounds what is read; a gate file is a few hundred bytes.
	maxFileBytes = 1 << 20
)

// Repo is a loaded and validated heavyq.toml, or, when the repository has
// none, the default gate Detect found (Path is then empty).
type Repo struct {
	Root   string // git top level the file was found in
	Path   string // Root/heavyq.toml; "" for a detected gate
	Gates  []Gate // file order
	Anchor Anchor
	// Proof says where proofs run by default; its Remote is "" when the
	// file has no [proof] remote (the user's config, then local, decide).
	Proof Proof
	// Items are the [items] limits of the claim size gate; defaults when absent.
	Items Items
	// Pools are the [[pool]] tables of the file; they replace pools of the
	// same name in the user's config.
	Pools []pools.Pool
}

// Gate is a named list of commands that must all pass on the current tree.
type Gate struct {
	Name   string
	Run    []string // shell strings, run with `sh -c`, in order
	Prio   int      // 1 runs first; DefaultPrio when unset
	Light  bool     // may use the light slots
	Remote string   // "local" | "auto" | <remote name>; DefaultRemote when unset
	// RemoteSet is true when the gate has its own remote key. A gate without
	// one follows [proof] remote (see cli.resolveProofRemote).
	RemoteSet bool
	// Needs are pool values every command of the gate holds while it runs
	// ("pg", "port:2"). A gate with needs runs on this machine.
	Needs []string
	// With are sibling repositories every command needs next to this one,
	// "name@ref" or "name" (HEAD); they are passed to the run as --with.
	With []string
	// NeedGH: the commands use the GitHub CLI login of this machine, so the
	// gate runs here (--need-gh).
	NeedGH bool
	// Tools are commands every command of the gate needs on the machine it
	// runs on; a remote without one is skipped (--tools, docs/remote.md).
	Tools []string
	// Paths are the files whose content is the gate's proof identity: globs relative to the repository root
	// (see package gatepaths). A commit that changes none of them keeps the gate proven. Empty: the whole tree.
	Paths []string
	// Detected marks the default gate Detect made up from files at the git
	// root because the repository has no heavyq.toml; Sources names those
	// files. Both are zero for a gate read from a file.
	Detected bool
	Sources  []string
}

// Anchor is the [anchor] section: where the checklist of `heavyq item`,
// flow and steer lives (anchor.Load reads it; File defaults to
// .heavyq/anchor.md), and the extra read-only sources of items.
type Anchor struct {
	File  string   // checklist file, relative to Root
	Extra []string // extra globs, relative to Root
}

// Proof is the [proof] section of heavyq.toml: where `heavyq item done --run`
// and `heavyq gate run` run proofs for this repository.
type Proof struct {
	Remote string // "local" | "auto" | <remote name> | an ordered list "a,b,c"; "" when not set
}

// Defaults of the [items] section.
const (
	DefaultMaxProofShare = 1.0 // the whole lease may be spent on proof runs
	DefaultIterations    = 3   // proof runs an item is expected to take before it passes
	MaxIterations        = 20
)

// The values of [items] tick: whether `heavyq item done` ticks the box in the anchor file.
const (
	// TickAuto ticks unless the repository has more than one work tree: parallel lanes
	// ticking adjacent lines of one file conflict when their branches merge (#119).
	TickAuto = "auto"
	// TickAlways ticks whatever the work trees are.
	TickAlways = "always"
	// TickNever only records the done in the events log; `heavyq item sync` ticks.
	TickNever = "never"
)

// Items is the [items] section: the limits `heavyq item claim` holds an item to,
// and whether `heavyq item done` ticks the box.
type Items struct {
	// Tick is TickAuto (the default), TickAlways or TickNever.
	Tick string
	// MaxProofShare is the share of the lease budget (30 min) that the expected
	// proof time may use, in (0, 1].
	MaxProofShare float64
	// Iterations is how many proof runs an item is expected to need; the
	// expected proof time is the proof's p50 duration times this.
	Iterations int
}

// DefaultItems are the limits when [items] is absent or leaves a key out.
func DefaultItems() Items {
	return Items{MaxProofShare: DefaultMaxProofShare, Iterations: DefaultIterations, Tick: TickAuto}
}

// ItemLimits is the [items] limits of r, the defaults when r is nil (no
// heavyq.toml in the repository).
func (r *Repo) ItemLimits() Items {
	if r == nil || r.Items.Iterations == 0 {
		return DefaultItems()
	}
	return r.Items
}

// Gate returns the gate called name.
func (r *Repo) Gate(name string) (Gate, bool) {
	for _, g := range r.Gates {
		if g.Name == name {
			return g, true
		}
	}
	return Gate{}, false
}

// Where names the source of r's gates in messages: the file, or the
// detection at the repository root.
func (r *Repo) Where() string {
	if r.Path != "" {
		return r.Path
	}
	return "detected defaults at " + r.Root
}

// rawFile mirrors the TOML layout before validation. Prio is a pointer so
// that "absent" (default 3) differs from an explicit, invalid 0.
type rawFile struct {
	Gate   []rawGate    `toml:"gate"`
	Pool   []pools.Spec `toml:"pool"`
	Anchor *rawAnchor   `toml:"anchor"`
	Proof  *rawProof    `toml:"proof"`
	Items  *rawItems    `toml:"items"`
}

type rawProof struct {
	Remote string `toml:"remote"`
}

// rawItems mirrors [items]; pointers tell "absent" (the default) from an invalid 0.
type rawItems struct {
	MaxProofShare *float64 `toml:"max_proof_share"`
	Iterations    *int     `toml:"iterations"`
	Tick          *string  `toml:"tick"`
}

type rawGate struct {
	Name   string   `toml:"name"`
	Run    []string `toml:"run"`
	Prio   *int     `toml:"prio"`
	Light  bool     `toml:"light"`
	Remote string   `toml:"remote"`
	Needs  []string `toml:"needs"`
	With   []string `toml:"with"`
	NeedGH bool     `toml:"need_gh"`
	Tools  []string `toml:"tools"`
	Paths  []string `toml:"paths"`
}

type rawAnchor struct {
	File  string   `toml:"file"`
	Extra []string `toml:"extra"`
}
