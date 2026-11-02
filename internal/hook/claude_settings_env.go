package hook

import "errors"

// MergeEnv returns settings whose "env" block sets key to value (Claude Code
// exports that block to every session), and whether anything changed. Every
// other key is kept. A settings file that is not a JSON object, or whose "env"
// is not an object, is refused and left to the caller to report.
func MergeEnv(existing []byte, key, value string) ([]byte, bool, error) {
	root, err := decodeRoot(existing)
	if err != nil {
		return nil, false, err
	}
	env := map[string]any{}
	if raw, ok := root["env"]; ok {
		m, ok := raw.(map[string]any)
		if !ok {
			return nil, false, errors.New(`"env" is not an object`)
		}
		env = m
	}
	if cur, ok := env[key].(string); ok && cur == value {
		return existing, false, nil
	}
	env[key] = value
	root["env"] = env
	b, err := encodeSettings(root)
	return b, true, err
}
