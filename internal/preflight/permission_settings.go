package preflight

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// maxSettingsBytes bounds a settings file read.
const maxSettingsBytes = 1 << 20

// SettingsPaths are the Claude Code settings files that can hold permission
// rules for a work tree: the project's two and the user's. claudeDir is
// $CLAUDE_CONFIG_DIR or ~/.claude ("" skips the user file).
func SettingsPaths(root, claudeDir string) []string {
	paths := []string{
		filepath.Join(root, ".claude", "settings.json"),
		filepath.Join(root, ".claude", "settings.local.json"),
	}
	if claudeDir != "" {
		paths = append(paths, filepath.Join(claudeDir, "settings.json"))
	}
	return paths
}

// LoadRules merges the rules of every file of paths that exists. found lists
// the files that were read; problems says which could not be (unreadable, not
// JSON), whose rules are then missing: the checks stay conservative.
func LoadRules(paths []string) (r Rules, found, problems []string) {
	for _, p := range paths {
		f, err := os.Open(p)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s: %v", p, err))
			continue
		}
		b, err := io.ReadAll(io.LimitReader(f, maxSettingsBytes))
		f.Close()
		if err == nil {
			err = r.Merge(b)
		}
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s: %v", p, err))
			continue
		}
		found = append(found, p)
	}
	return r, found, problems
}
