// Package repoconfig loads the per-repository file heavyq.toml, which lives
// at the git root and says what "proven" means for that repository: named
// gates, each a list of shell commands.
//
// Trust model: heavyq.toml is trusted like a Makefile. Its commands are run
// only by an explicit `heavyq gate run`, never by a hook. Everything in it is
// still validated and unknown keys are refused, so a typo cannot silently
// weaken a gate.
package repoconfig

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

// Repo is a loaded and validated heavyq.toml.
type Repo struct {
	Root   string // git top level the file was found in
	Path   string // Root/heavyq.toml
	Gates  []Gate // file order
	Anchor Anchor
}

// Gate is a named list of commands that must all pass on the current tree.
type Gate struct {
	Name   string
	Run    []string // shell strings, run with `sh -c`, in order
	Prio   int      // 1 runs first; DefaultPrio when unset
	Light  bool     // may use the light slots
	Remote string   // "local" | "auto" | <remote name>; DefaultRemote when unset
}

// Anchor is the reserved [anchor] section. It is parsed and validated so
// files written today keep working, but nothing reads it yet.
type Anchor struct {
	File  string   // checklist file, relative to Root
	Extra []string // extra globs, relative to Root
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

// rawFile mirrors the TOML layout before validation. Prio is a pointer so
// that "absent" (default 3) differs from an explicit, invalid 0.
type rawFile struct {
	Gate   []rawGate  `toml:"gate"`
	Anchor *rawAnchor `toml:"anchor"`
}

type rawGate struct {
	Name   string   `toml:"name"`
	Run    []string `toml:"run"`
	Prio   *int     `toml:"prio"`
	Light  bool     `toml:"light"`
	Remote string   `toml:"remote"`
}

type rawAnchor struct {
	File  string   `toml:"file"`
	Extra []string `toml:"extra"`
}
