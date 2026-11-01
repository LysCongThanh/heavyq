package repoconfig

import (
	"slices"
	"strings"

	"github.com/BurntSushi/toml"
)

// firstUnknownKey returns the first key the schema does not define, or nil.
//
// BurntSushi/toml matches keys to struct fields case-insensitively, so
// "[[GATE]]" or "Run = ..." would decode without a word. Worse, "[[gaTe]]"
// and "[[gate]]" in one file fill the same list and one silently replaces the
// other (which one is random), dropping a gate. Every key of the schema is
// lower case, so a key with an upper-case letter is refused as unknown.
func firstUnknownKey(md toml.MetaData) toml.Key {
	if undec := md.Undecoded(); len(undec) > 0 {
		return undec[0]
	}
	for _, key := range md.Keys() {
		for _, part := range key {
			if part != strings.ToLower(part) {
				return key
			}
		}
	}
	return nil
}

// unknownKeyLine returns the 1-based line where the undecoded key is
// written, or 0 when it cannot be found. The TOML library reports unknown
// keys without positions, so this scans the text, tracking the current
// [table] / [[array of tables]] header. It is a best-effort aid for error
// messages: it understands plain and quoted keys, not quoted keys that
// contain dots.
func unknownKeyLine(data []byte, key toml.Key) int {
	var table []string
	for i, raw := range strings.Split(string(data), "\n") {
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
			if len(full) >= len(key) && slices.Equal(full[:len(key)], key) {
				return i + 1
			}
		}
	}
	return 0
}

// headerPath returns the key path of a "[a.b]" or "[[a.b]]" header line.
func headerPath(line string) []string {
	closer := "]"
	if strings.HasPrefix(line, "[[") {
		line, closer = line[2:], "]]"
	} else {
		line = line[1:]
	}
	if end := strings.Index(line, closer); end >= 0 {
		line = line[:end]
	}
	return splitKey(line)
}

// splitKey splits a dotted key into its parts, dropping quotes and spaces.
func splitKey(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ".") {
		p = strings.TrimSpace(p)
		p = strings.Trim(p, `"'`)
		out = append(out, p)
	}
	return out
}
