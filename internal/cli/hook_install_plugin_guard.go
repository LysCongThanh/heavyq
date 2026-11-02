package cli

import (
	"fmt"
	"os"

	"github.com/LysCongThanh/heavyq/internal/hook"
)

// pluginEnabledIn returns the first Claude Code settings file (user, project,
// local) that enables the heavyq plugin, or "" when none does. A file that
// cannot be read or resolved is skipped: the guard only ever adds a refusal
// on positive evidence.
func pluginEnabledIn() string {
	for _, scope := range []string{"user", "project", "local"} {
		path, err := settingsFile(scope)
		if err != nil {
			continue
		}
		b, err := os.ReadFile(path)
		if err == nil && hook.PluginEnabled(b) {
			return path
		}
	}
	return ""
}

// pluginRefusal is the stderr text of the double-install refusal (exit 2).
func pluginRefusal(path string) string {
	return fmt.Sprintf("heavyq: the Claude Code plugin %s is enabled in %s, and it already runs the same hook; installing it into settings too would run it twice\n"+
		"next: keep the plugin (nothing to do), or disable it (/plugin disable %s) and run this again; --force installs anyway\n",
		hook.PluginID, path, hook.PluginID)
}
