package hook

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

// Subcommand is what heavyq is called with from Claude Code; it also marks
// the entries this installer owns.
const Subcommand = "hook claude-pretooluse"

// hookTimeoutSecs bounds the hook itself. A timed-out PreToolUse hook does
// not block the call (Claude Code docs), so this only caps a stall.
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
// running command, and whether anything changed.
func MergeInstall(existing []byte, command string) ([]byte, bool, error) {
	root, pre, err := decodeSettings(existing)
	if err != nil {
		return nil, false, err
	}
	kept, removed := withoutOurs(pre)
	want := map[string]any{"type": "command", "command": command, "timeout": json.Number(fmt.Sprint(hookTimeoutSecs))}
	kept = append(kept, map[string]any{"matcher": "Bash", "hooks": []any{want}})
	if removed == 1 && ourEntryIs(pre, command) {
		return existing, false, nil
	}
	setPreToolUse(root, kept)
	b, err := encodeSettings(root)
	return b, true, err
}

// MergeUninstall removes every heavyq PreToolUse entry.
func MergeUninstall(existing []byte) ([]byte, bool, error) {
	root, pre, err := decodeSettings(existing)
	if err != nil {
		return nil, false, err
	}
	kept, removed := withoutOurs(pre)
	if removed == 0 {
		return existing, false, nil
	}
	setPreToolUse(root, kept)
	b, err := encodeSettings(root)
	return b, true, err
}

func decodeSettings(b []byte) (map[string]any, []any, error) {
	root := map[string]any{}
	if len(bytes.TrimSpace(b)) > 0 {
		dec := json.NewDecoder(bytes.NewReader(b))
		dec.UseNumber()
		if err := dec.Decode(&root); err != nil {
			return nil, nil, fmt.Errorf("settings are not a JSON object: %w", err)
		}
	}
	hooks, ok := root["hooks"]
	if !ok {
		return root, nil, nil
	}
	hm, ok := hooks.(map[string]any)
	if !ok {
		return nil, nil, errors.New(`"hooks" is not an object`)
	}
	pre, ok := hm["PreToolUse"]
	if !ok {
		return root, nil, nil
	}
	arr, ok := pre.([]any)
	if !ok {
		return nil, nil, errors.New(`"hooks.PreToolUse" is not an array`)
	}
	return root, arr, nil
}

func isOurs(h any) bool {
	m, ok := h.(map[string]any)
	if !ok {
		return false
	}
	c, _ := m["command"].(string)
	return strings.HasSuffix(strings.TrimSpace(c), Subcommand)
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

// ourEntryIs reports whether the single heavyq entry is already exactly the
// one MergeInstall would write, alone in a "Bash" group.
func ourEntryIs(pre []any, command string) bool {
	for _, g := range pre {
		gm, _ := g.(map[string]any)
		hs, _ := gm["hooks"].([]any)
		for _, h := range hs {
			if !isOurs(h) {
				continue
			}
			hm := h.(map[string]any)
			return gm["matcher"] == "Bash" && len(hs) == 1 && hm["command"] == command &&
				hm["type"] == "command" && fmt.Sprint(hm["timeout"]) == fmt.Sprint(hookTimeoutSecs)
		}
	}
	return false
}

func setPreToolUse(root map[string]any, pre []any) {
	hm, _ := root["hooks"].(map[string]any)
	if hm == nil {
		hm = map[string]any{}
	}
	if len(pre) == 0 {
		delete(hm, "PreToolUse")
	} else {
		hm["PreToolUse"] = pre
	}
	if len(hm) == 0 {
		delete(root, "hooks")
	} else {
		root["hooks"] = hm
	}
}

func encodeSettings(root map[string]any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	err := enc.Encode(root)
	return buf.Bytes(), err
}
