// Package gatepaths is the shape of a gate's `paths` in heavyq.toml: the files whose content makes up the gate's
// proof identity. A gate that declares them is proven again by any commit that changes none of the files they
// name, so a docs-only commit keeps the proof of a Go gate.
//
// The syntax is the subset of git's `:(glob)` pathspec that means the same everywhere (git, the validator here,
// the schema): literal characters, `*` and `?` inside one path component, and `**` as a whole component for
// "any number of directories". A path with no wildcard names a file, or a directory and everything below it.
// Character classes and escapes are refused instead of guessed at, and so is anything that could leave the
// repository, so an unknown shape fails closed.
package gatepaths

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

// Limits of one gate's `paths`.
const (
	MaxEntries = 64  // patterns per gate
	MaxLen     = 200 // bytes per pattern
)

// Valid says why p is not a usable entry of a gate's `paths` (nil when it is).
func Valid(p string) error {
	switch {
	case p == "":
		return errors.New("is empty")
	case len(p) > MaxLen:
		return fmt.Errorf("%q is %d bytes; the limit is %d", p[:40]+"...", len(p), MaxLen)
	case strings.HasPrefix(p, "/"):
		return fmt.Errorf("%q must be relative to the repository root", p)
	case strings.HasSuffix(p, "/"):
		return fmt.Errorf("%q must not end with a slash: write %q for everything below a directory", p, p+"**")
	case strings.HasPrefix(p, ":") || strings.HasPrefix(p, "!"):
		return fmt.Errorf("%q must not start with a colon or an exclamation mark", p)
	}
	for _, r := range p {
		switch {
		case r < 0x20 || r == 0x7f:
			return fmt.Errorf("%q contains a control character", p)
		case r == '\\' || r == '[' || r == ']':
			return fmt.Errorf("%q: character classes and escapes are not supported; use * and ? or list the files", p)
		}
	}
	for _, el := range strings.Split(p, "/") {
		switch {
		case el == "" || el == "." || el == "..":
			return fmt.Errorf("%q must not have an empty, \".\" or \"..\" path component", p)
		case strings.Contains(el, "**") && el != "**":
			return fmt.Errorf("%q: ** must be a whole path component (a/**/b, **/*.go, dir/**)", p)
		}
	}
	return nil
}

// ValidList checks a gate's whole `paths`: at most MaxEntries valid entries, none twice.
func ValidList(paths []string) error {
	if len(paths) > MaxEntries {
		return fmt.Errorf("%d entries; the limit is %d", len(paths), MaxEntries)
	}
	seen := map[string]bool{}
	for i, p := range paths {
		if err := Valid(p); err != nil {
			return fmt.Errorf("[%d] %w", i, err)
		}
		if seen[p] {
			return fmt.Errorf("[%d] %q is listed twice", i, p)
		}
		seen[p] = true
	}
	return nil
}

// Canonical is paths sorted: the order a gate lists them in never changes the identity.
func Canonical(paths []string) []string {
	out := slices.Clone(paths)
	slices.Sort(out)
	return slices.Compact(out)
}
