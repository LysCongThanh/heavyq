package gitutil

import (
	"io"
	"os"
	"path"
	"path/filepath"

	"github.com/BurntSushi/toml"

	"github.com/LysCongThanh/heavyq/internal/anchor"
)

// configFile is heavyq's own config. It is deliberately NOT bookkeeping:
// editing a gate must void the proof that was made under the old one.
const configFile = "heavyq.toml"

// maxConfigBytes bounds the heavyq.toml read here (repoconfig has its own cap).
const maxConfigBytes = 1 << 20

// bookkeepingDirs are the directories heavyq itself writes into. Ticking an
// item, recording a decision or a stance is bookkeeping about the work, not
// work: it must not change what the code is, or `heavyq item done` would
// void the proof that justified it.
var bookkeepingDirs = []string{".heavyq/decisions", ".heavyq/stances"}

// bookkeepingPaths returns the repository-relative, slash-separated paths
// (files or directories) left out of every tree identity: the configured
// anchor file ([anchor] file in heavyq.toml, default anchor.DefaultFile) and
// bookkeepingDirs.
//
// The config is read from the work tree, or from commit when commit is not ""
// (a pinned remote run has no work tree of its own). An absent, unreadable or
// invalid config means the default anchor file: this only decides what is
// ignored, never whether a run may happen, and an edit of heavyq.toml changes
// the identity anyway. The anchor file is never heavyq.toml itself.
func bookkeepingPaths(repo, commit string) []string {
	file := anchor.DefaultFile
	if data, ok := readConfig(repo, commit); ok {
		var c struct {
			Anchor struct {
				File string `toml:"file"`
			} `toml:"anchor"`
		}
		if _, err := toml.Decode(string(data), &c); err == nil && c.Anchor.File != "" && filepath.IsLocal(c.Anchor.File) {
			if p := path.Clean(filepath.ToSlash(c.Anchor.File)); p != configFile {
				file = p
			}
		}
	}
	return append([]string{file}, bookkeepingDirs...)
}

// readConfig returns heavyq.toml's content, from the work tree or from commit.
func readConfig(repo, commit string) ([]byte, bool) {
	if commit != "" {
		out, err := gitRaw(repo, "show", commit+":"+configFile)
		return []byte(out), err == nil && len(out) <= maxConfigBytes
	}
	f, err := os.Open(filepath.Join(repo, configFile))
	if err != nil {
		return nil, false
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxConfigBytes+1))
	return data, err == nil && len(data) <= maxConfigBytes
}

// excludeSpecs turns paths into git pathspec arguments (starting at "--")
// that leave them out of a diff, a status or a file listing. "top" and
// "literal" keep each one root-relative and free of glob magic wherever git
// runs.
func excludeSpecs(paths []string) []string {
	specs := []string{"--"}
	for _, p := range paths {
		specs = append(specs, ":(top,exclude,literal)"+p)
	}
	return specs
}
