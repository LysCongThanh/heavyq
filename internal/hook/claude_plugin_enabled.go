package hook

import (
	"bytes"
	"encoding/json"
)

// PluginID is the install id of the heavyq Claude Code plugin
// (`/plugin install heavyq@heavyq`): the plugin name, "@", the marketplace
// name, as written under enabledPlugins in a Claude Code settings file.
const PluginID = "heavyq@heavyq"

// PluginEnabled reports whether a Claude Code settings file turns the heavyq
// plugin on. The plugin ships the same PreToolUse hook that `heavyq hook
// install claude` writes, and hooks from every source run in parallel with no
// defined winner when two of them rewrite the command, so installing both is
// refused. Anything unreadable (empty, not JSON, wrong types) counts as
// "not enabled": the installer reports a malformed file on its own.
func PluginEnabled(settings []byte) bool {
	if len(bytes.TrimSpace(settings)) == 0 {
		return false
	}
	var v struct {
		EnabledPlugins map[string]json.RawMessage `json:"enabledPlugins"`
	}
	if err := json.Unmarshal(settings, &v); err != nil {
		return false
	}
	raw, ok := v.EnabledPlugins[PluginID]
	if !ok {
		return false
	}
	var on bool
	return json.Unmarshal(raw, &on) == nil && on
}
