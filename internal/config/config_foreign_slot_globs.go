package config

import (
	"fmt"
	"slices"
	"strings"
)

// SlotGlobClass is the one character class a slot glob may use: a decimal digit.
const SlotGlobClass = "[0-9]"

// checkSlotGlobs validates slot_globs: a configured machine, not also given a slot_dirs entry, and a glob
// CheckSlotGlob accepts.
func (f *ForeignConfig) checkSlotGlobs(machines []string) error {
	for m, g := range f.SlotGlobs {
		if !slices.Contains(machines, m) {
			return fmt.Errorf("slot_globs: %q is not a configured machine (%s)", m, strings.Join(machines, ", "))
		}
		if _, ok := f.SlotDirs[m]; ok {
			return fmt.Errorf("slot_globs: %s has slot_dirs too; give one of them", m)
		}
		if err := CheckSlotGlob(g); err != nil {
			return fmt.Errorf("slot_globs: %q for %s: %w", g, m, err)
		}
	}
	return nil
}

// CheckSlotGlob accepts a glob that names files of one directory and nothing else: absolute or "~/", a
// directory part without wildcards, "..", "~" (past its "~/") or shell metacharacters, and a base name of
// [A-Za-z0-9._-] with exactly the wildcards one "*" and any number of "[0-9]" (at least one wildcard, at
// least one plain character, not starting with "."). Neither wildcard matches "/", and a base name that
// cannot start with "." never matches "." or "..": the glob cannot leave its directory. The probe also
// skips symbolic links, so a planted link cannot point it elsewhere.
func CheckSlotGlob(g string) error {
	if !strings.HasPrefix(g, "/") && !strings.HasPrefix(g, "~/") {
		return fmt.Errorf("must be absolute or start with ~/")
	}
	i := strings.LastIndex(g, "/")
	dir, base := g[:i+1], g[i+1:]
	if !safeRemoteDir(dir) || strings.ContainsAny(dir, "[]") || strings.Contains(dir[1:], "~") {
		return fmt.Errorf("the directory part has a wildcard, \"..\", \"~\" or unsafe characters")
	}
	if base == "" || base[0] == '.' {
		return fmt.Errorf("the base name must be set and must not start with \".\"")
	}
	stars, classes, plain := 0, 0, 0
	for rest := base; rest != ""; {
		switch c := rest[0]; {
		case strings.HasPrefix(rest, SlotGlobClass):
			classes++
			rest = rest[len(SlotGlobClass):]
			continue
		case c == '*':
			stars++
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '.', c == '_', c == '-':
			plain++
		default:
			return fmt.Errorf("the base name may hold [A-Za-z0-9._-], one \"*\" and %q only, not %q", SlotGlobClass, c)
		}
		rest = rest[1:]
	}
	switch {
	case stars > 1:
		return fmt.Errorf("one \"*\" at most")
	case stars+classes == 0:
		return fmt.Errorf("no wildcard: use \"*\" or %q", SlotGlobClass)
	case plain == 0:
		return fmt.Errorf("the base name needs a plain part, not only wildcards")
	}
	return nil
}
