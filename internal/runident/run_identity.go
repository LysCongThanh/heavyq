// Package runident defines what "the same run" means, once: the run_id.
//
// Two runs are the same run when they execute the same exact argv, from the
// same directory of the same code (git tree id), with the same value for every
// variable of the cache.env allowlist, against the same base ref commit.
// Where the run executed (local or a remote machine) is deliberately not part
// of it: the code and the command decide the result, not the machine.
//
// The result cache keys on it, every ledger record carries it, and proof,
// flaky detection and loop detection match on it. Nothing else compares
// commands.
package runident

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"

	"github.com/LysCongThanh/heavyq/internal/gitutil"
	"github.com/LysCongThanh/heavyq/internal/history"
)

// schema versions the encoding below. Changing the encoding changes every
// run_id; bump it so old ids simply never match (they fall back, see the
// proof package). Schema 2: the tree id ignores heavyq's bookkeeping files
// (anchor file, decisions, stances), see gitutil.effectiveTreeOf.
const schema = 2

// Input is everything that makes two runs the same run.
type Input struct {
	// Tree is the exact code: gitutil.Checkout.Tree, clean or dirty. "" means
	// unknown, and an unknown tree has no identity.
	Tree string
	// Argv is the exact argv as given, before any redaction or truncation.
	Argv []string
	// Subdir is the directory relative to the work tree root ("" at the root).
	Subdir string
	// EnvNames is the cache.env allowlist: the variables whose values count.
	EnvNames []string
	// Getenv reads a variable (os.LookupEnv); nil reads none, so every
	// allowlisted variable counts as unset.
	Getenv func(string) (string, bool)
	// BaseSHA is the commit of the --base-ref (see BaseSHA); "" for none.
	BaseSHA string
	// Siblings are the pinned --with repositories ("name@sha"), if any.
	Siblings []string
}

// doc is hashed as canonical JSON (struct order is fixed, map keys sorted).
type doc struct {
	Schema   int               `json:"schema"`
	Tree     string            `json:"tree"`
	CmdHash  string            `json:"cmd_hash"` // history.CmdHash: sha256 of the NUL-joined argv
	Subdir   string            `json:"subdir"`
	Env      map[string]string `json:"env"`
	Unset    []string          `json:"unset"`
	BaseSHA  string            `json:"base_sha"`
	Siblings []string          `json:"siblings,omitempty"`
}

// ID returns the run_id: a hex sha256, or "" when the tree is unknown (a run
// on code nobody can name is never the same as another run).
func ID(in Input) string {
	if in.Tree == "" {
		return ""
	}
	d := doc{Schema: schema, Tree: in.Tree, CmdHash: history.CmdHash(in.Argv), Subdir: in.Subdir,
		Env: map[string]string{}, Unset: []string{}, BaseSHA: in.BaseSHA}
	for _, n := range sortedUnique(in.EnvNames) {
		if v, ok := lookup(in.Getenv, n); ok {
			d.Env[n] = v
		} else {
			d.Unset = append(d.Unset, n)
		}
	}
	if len(in.Siblings) > 0 {
		d.Siblings = sortedUnique(in.Siblings)
	}
	b, err := json.Marshal(d)
	if err != nil { // only strings and maps of strings: cannot happen
		return ""
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// BaseSHA is the commit of refs/remotes/<baseRef> in the repository at dir.
// No base ref, and a base ref that does not resolve, are the same thing to a
// run (the command sees no base), so both give "".
func BaseSHA(dir, baseRef string) string {
	if baseRef == "" {
		return ""
	}
	sha, err := gitutil.ResolveCommit(dir, "refs/remotes/"+baseRef)
	if err != nil {
		return ""
	}
	return sha
}

func lookup(get func(string) (string, bool), name string) (string, bool) {
	if get == nil {
		return "", false
	}
	return get(name)
}

func sortedUnique(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	n := 0
	for i, s := range out {
		if i == 0 || s != out[n-1] {
			out[n] = s
			n++
		}
	}
	return out[:n]
}
