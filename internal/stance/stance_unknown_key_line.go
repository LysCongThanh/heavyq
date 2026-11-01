package stance

import (
	"slices"
	"strings"

	"github.com/BurntSushi/toml"
)

// unknownKeyLine returns the 1-based line (within text) where the undecoded
// key is written, or 0 when it cannot be found. The TOML library reports
// unknown keys without positions, so this scans the text, tracking the
// current [table] header. A line matches when its full key path equals the
// key or is a prefix of it (an inline table such as `stall = {x = 1}` holds
// the unknown key `enter.stall.x` on the line of `stall`). Best effort: it
// understands plain and quoted keys, not quoted keys that contain dots.
func unknownKeyLine(text string, key toml.Key) int {
	var table []string
	for i, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		switch {
		case line == "" || strings.HasPrefix(line, "#"):
			continue
		case strings.HasPrefix(line, "["):
			table = headerPath(line)
			if slices.Equal(table, key) {
				return i + 1
			}
		default:
			eq := strings.IndexByte(line, '=')
			if eq < 0 {
				continue
			}
			full := append(slices.Clone(table), splitKey(line[:eq])...)
			if len(full) <= len(key) && slices.Equal(full, key[:len(full)]) {
				return i + 1
			}
		}
	}
	return 0
}

// headerPath returns the key path of a "[a.b]" header line.
func headerPath(line string) []string {
	line = strings.TrimPrefix(line, "[")
	if end := strings.IndexByte(line, ']'); end >= 0 {
		line = line[:end]
	}
	return splitKey(line)
}

// splitKey splits a dotted key into its parts, dropping quotes and spaces.
func splitKey(s string) []string {
	parts := strings.Split(s, ".")
	for i, p := range parts {
		parts[i] = strings.Trim(strings.TrimSpace(p), `"'`)
	}
	return parts
}
