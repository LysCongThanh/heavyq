// Package pathglob matches repository-relative, slash-separated file paths
// against the globs a gate's `paths` key declares (see docs/gates.md).
//
// The syntax is the one of path.Match, per path element, plus "**" as a whole
// element: it matches zero or more directories. A pattern is anchored at the
// repository root (there is no implicit "any depth"): "*.go" is the Go files
// at the root, "**/*.go" those at any depth, "go.mod" the root's go.mod and
// "docs/**" everything below docs/. The same input always gives the same
// answer on every machine: no locale, no case folding, no file system.
package pathglob

import (
	"errors"
	"fmt"
	"path"
	"strings"
)

const doubleStar = "**"

// Validate reports why pattern is not a usable glob: empty, absolute, with an
// empty, "." or ".." element, a backslash, a "**" that is not a whole
// element, or a path.Match syntax error.
func Validate(pattern string) error {
	switch {
	case strings.TrimSpace(pattern) == "":
		return errors.New("is empty")
	case strings.ContainsAny(pattern, "\\\x00"):
		return errors.New("must not contain a backslash or a NUL byte: paths are slash-separated")
	case strings.HasPrefix(pattern, "/"):
		return errors.New("must be relative to the repository root, not absolute")
	case strings.HasSuffix(pattern, "/"):
		return errors.New(`must name files, not a directory: use "dir/**" for everything below it`)
	}
	for _, el := range strings.Split(pattern, "/") {
		switch {
		case el == "" || el == "." || el == "..":
			return fmt.Errorf("has an empty, %q or %q element", ".", "..")
		case el != doubleStar && strings.Contains(el, doubleStar):
			return errors.New(`"**" must be a whole path element, as in "**/*.go"`)
		}
		if _, err := path.Match(el, ""); err != nil {
			return err
		}
	}
	return nil
}

// Matcher is a compiled list of patterns; the zero value matches nothing.
type Matcher struct {
	patterns [][]string // each pattern, split into elements
}

// Compile validates patterns and returns their Matcher.
func Compile(patterns []string) (Matcher, error) {
	m := Matcher{patterns: make([][]string, 0, len(patterns))}
	for _, p := range patterns {
		if err := Validate(p); err != nil {
			return Matcher{}, fmt.Errorf("%q: %w", p, err)
		}
		m.patterns = append(m.patterns, strings.Split(p, "/"))
	}
	return m, nil
}

// Match reports whether name (slash-separated, relative to the root) matches
// any pattern.
func (m Matcher) Match(name string) bool {
	els := strings.Split(name, "/")
	for _, p := range m.patterns {
		if matchElements(p, els) {
			return true
		}
	}
	return false
}

// matchElements matches the path elements els against the pattern elements pat.
func matchElements(pat, els []string) bool {
	for len(pat) > 0 {
		if pat[0] == doubleStar {
			rest := pat[1:]
			if len(rest) == 0 {
				return len(els) > 0 // "dir/**" is the files below dir, never dir itself
			}
			for i := 0; i <= len(els); i++ {
				if matchElements(rest, els[i:]) {
					return true
				}
			}
			return false
		}
		if len(els) == 0 {
			return false
		}
		if ok, err := path.Match(pat[0], els[0]); err != nil || !ok {
			return false
		}
		pat, els = pat[1:], els[1:]
	}
	return len(els) == 0
}
