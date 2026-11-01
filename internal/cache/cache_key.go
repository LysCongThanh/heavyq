// Package cache stores the output and exit code of commands keyed by the
// exact committed code they ran on, and coalesces identical in-flight runs.
package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/LysCongThanh/heavyq/internal/gitutil"
)

// KeyInput is everything that identifies a cacheable run.
type KeyInput struct {
	Dir      string   // where the command runs
	Argv     []string // exact argv
	EnvNames []string // allowlisted variables whose values are part of the key
	Version  string   // heavyq version
	BaseRef  string   // --base-ref (remote/branch): its commit joins the key
	Getenv   func(string) (string, bool)
}

// keyDoc is hashed as canonical JSON. Changing it invalidates old entries,
// which is safe (they simply miss).
type keyDoc struct {
	Schema  int               `json:"schema"`
	Tree    string            `json:"tree"`
	Clean   bool              `json:"clean"`
	Argv    []string          `json:"argv"`
	Subdir  string            `json:"subdir"`
	Env     map[string]string `json:"env"`
	Unset   []string          `json:"unset"`
	Version string            `json:"version"`
	BaseSHA string            `json:"base_sha,omitempty"`
}

// Key returns the cache key of a run, or "" and the reason the run cannot
// be cached (not in a repo, or the work tree differs from HEAD).
func Key(in KeyInput) (string, string) {
	top, err := gitutil.TopLevel(in.Dir)
	if err != nil {
		return "", "not in a git repository"
	}
	if !gitutil.IsClean(top) {
		return "", "work tree has uncommitted or untracked files"
	}
	tree, err := gitutil.TreeOf(top, "HEAD")
	if err != nil {
		return "", "no HEAD commit"
	}
	sub, err := gitutil.Prefix(in.Dir)
	if err != nil {
		return "", "cannot resolve the subdirectory"
	}
	d := keyDoc{Schema: 1, Tree: tree, Clean: true, Argv: in.Argv, Subdir: sub,
		Env: map[string]string{}, Version: in.Version}
	if in.BaseRef != "" {
		d.BaseSHA = "missing"
		if sha, err := gitutil.ResolveCommit(top, "refs/remotes/"+in.BaseRef); err == nil {
			d.BaseSHA = sha
		}
	}
	for _, n := range in.EnvNames {
		if v, ok := in.Getenv(n); ok {
			d.Env[n] = v
		} else {
			d.Unset = append(d.Unset, n)
		}
	}
	b, err := json.Marshal(d) // map keys are sorted: canonical
	if err != nil {
		return "", "cannot encode the key"
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), ""
}
