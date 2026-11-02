package hook

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
)

// Subcommand is what heavyq is called with from Claude Code for the queue
// hook; ownedSubcommand recognises every `hook claude-*` entry this installer
// owns, this one included.
const Subcommand = "hook claude-pretooluse"

// hookTimeoutSecs bounds the PreToolUse hook itself. A timed-out PreToolUse hook
// does not block the call (Claude Code docs), so this only caps a stall.
const hookTimeoutSecs = 30

// SettingsPath maps an install scope to its Claude Code settings file.
// root is the project root; configDir is ~/.claude (or $CLAUDE_CONFIG_DIR).
func SettingsPath(scope, root, configDir string) (string, error) {
	switch scope {
	case "local":
		return filepath.Join(root, ".claude", "settings.local.json"), nil
	case "project":
		return filepath.Join(root, ".claude", "settings.json"), nil
	case "user":
		return filepath.Join(configDir, "settings.json"), nil
	}
	return "", fmt.Errorf("--scope %q: want local, project or user", scope)
}

// MergeInstall returns settings with exactly one heavyq PreToolUse entry
// running command, and whether anything changed. It is MergeEntries for the
// default (queue) feature.
func MergeInstall(existing []byte, command string) ([]byte, bool, error) {
	return MergeEntries(existing, []Entry{{Event: "PreToolUse", Matcher: "Bash", Command: command, Timeout: hookTimeoutSecs}})
}

// MergeEntries returns settings in which exactly entries are the hooks heavyq
// owns: every other heavyq entry (an older binary path, a feature no longer
// wanted) is removed, foreign hooks and every other key are kept, and
// nothing is added twice. changed is false when the result means the same as
// existing, which is then returned byte for byte.
func MergeEntries(existing []byte, entries []Entry) ([]byte, bool, error) {
	root, hooks, err := decodeHooks(existing)
	if err != nil {
		return nil, false, err
	}
	for event, arr := range hooks {
		if kept, removed := withoutOurs(arr); removed > 0 {
			hooks[event] = kept
		}
	}
	for _, e := range entries {
		hooks[e.Event] = append(hooks[e.Event], entryGroup(e))
	}
	return finishMerge(existing, root, hooks)
}

// MergeUninstall removes every heavyq hook entry.
func MergeUninstall(existing []byte) ([]byte, bool, error) {
	root, hooks, err := decodeHooks(existing)
	if err != nil {
		return nil, false, err
	}
	removedAny := false
	for event, arr := range hooks {
		if kept, removed := withoutOurs(arr); removed > 0 {
			hooks[event], removedAny = kept, true
		}
	}
	if !removedAny {
		return existing, false, nil
	}
	return finishMerge(existing, root, hooks)
}

func entryGroup(e Entry) map[string]any {
	g := map[string]any{"hooks": []any{map[string]any{"type": "command", "command": e.Command,
		"timeout": json.Number(fmt.Sprint(e.Timeout))}}}
	if e.Matcher != "" {
		g["matcher"] = e.Matcher
	}
	return g
}

// finishMerge writes hooks back into root (dropping empty events and an empty
// "hooks"), and reports no change when the result is the same JSON value as
// the settings that came in.
func finishMerge(existing []byte, root map[string]any, hooks map[string][]any) ([]byte, bool, error) {
	hm, _ := root["hooks"].(map[string]any)
	if hm == nil {
		hm = map[string]any{}
	}
	for event, arr := range hooks {
		if orig, ok := hm[event].([]any); len(arr) == 0 && ok && len(orig) == 0 {
			continue // an empty list that was already there is none of our business
		}
		if len(arr) == 0 {
			delete(hm, event)
		} else {
			hm[event] = arr
		}
	}
	if len(hm) == 0 {
		delete(root, "hooks")
	} else {
		root["hooks"] = hm
	}
	before, _ := decodeRoot(existing)
	if reflect.DeepEqual(before, root) {
		return existing, false, nil
	}
	b, err := encodeSettings(root)
	return b, true, err
}

func decodeRoot(b []byte) (map[string]any, error) {
	root := map[string]any{}
	if len(bytes.TrimSpace(b)) > 0 {
		dec := json.NewDecoder(bytes.NewReader(b))
		dec.UseNumber()
		if err := dec.Decode(&root); err != nil {
			return nil, fmt.Errorf("settings are not a JSON object: %w", err)
		}
		if root == nil { // the JSON value null decodes to a nil map
			return nil, errors.New("settings are not a JSON object: null")
		}
	}
	return root, nil
}

// decodeHooks returns the settings and, per event, the array of hook groups.
// An event heavyq registers on that is not an array is an error; an unknown
// event of any other shape is left alone.
func decodeHooks(b []byte) (map[string]any, map[string][]any, error) {
	root, err := decodeRoot(b)
	if err != nil {
		return nil, nil, err
	}
	hooks := map[string][]any{}
	raw, ok := root["hooks"]
	if !ok {
		return root, hooks, nil
	}
	hm, ok := raw.(map[string]any)
	if !ok {
		return nil, nil, errors.New(`"hooks" is not an object`)
	}
	managed := managedEvents()
	for event, v := range hm {
		arr, ok := v.([]any)
		switch {
		case ok:
			hooks[event] = arr
		case managed[event]:
			return nil, nil, fmt.Errorf(`"hooks.%s" is not an array`, event)
		}
	}
	return root, hooks, nil
}

func isOurs(h any) bool {
	m, ok := h.(map[string]any)
	if !ok {
		return false
	}
	c, _ := m["command"].(string)
	name, ours := ownedSubcommand(c)
	return ours && ownsSubcommand(name)
}

// withoutOurs drops heavyq hooks (and matcher groups left empty by that).
func withoutOurs(pre []any) ([]any, int) {
	var kept []any
	removed := 0
	for _, g := range pre {
		gm, ok := g.(map[string]any)
		hs, ok2 := gm["hooks"].([]any)
		if !ok || !ok2 {
			kept = append(kept, g)
			continue
		}
		var left []any
		for _, h := range hs {
			if isOurs(h) {
				removed++
			} else {
				left = append(left, h)
			}
		}
		if len(left) == len(hs) {
			kept = append(kept, g)
		} else if len(left) > 0 {
			ng := map[string]any{}
			for k, v := range gm {
				ng[k] = v
			}
			ng["hooks"] = left
			kept = append(kept, ng)
		}
	}
	return kept, removed
}

func encodeSettings(root map[string]any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	err := enc.Encode(root)
	return buf.Bytes(), err
}
