package anchor

import (
	"path"
	"regexp"
	"strings"
)

// Limits of the scope spans, so a hostile line stays cheap to compare.
const (
	maxScopeGlobs = 32  // `paths:` globs kept per item
	maxScopeAfter = 32  // `after:` ids kept per item
	maxAfterIDLen = 128 // plan-file ids are longer than anchor ids
)

// afterID is what an `after:` entry may be: an anchor id, or a plan-file id
// (which has a "/").
var afterID = regexp.MustCompile(`^[a-z0-9][a-z0-9._/-]*$`)

// splitList cuts a span value at commas and white space.
func splitList(val string) []string {
	return strings.FieldsFunc(val, func(r rune) bool { return r == ',' || r == ' ' || r == '\t' })
}

// setPaths reads `paths: a/**, b/*.go`. A glob that is absolute, climbs out of
// the repository (`..`), or is malformed is dropped with a warning: it could
// not claim anything useful, and silently keeping it would hide a typo.
func (m *itemMeta) setPaths(val string) {
	if m.paths != nil {
		m.msgs = append(m.msgs, "more than one `paths:`; the first one is used")
		return
	}
	globs := splitList(val)
	if len(globs) == 0 {
		m.msgs = append(m.msgs, "empty `paths:` ignored")
		return
	}
	for _, g := range globs {
		g = strings.TrimPrefix(g, "./")
		switch {
		case len(m.paths) >= maxScopeGlobs:
			m.msgs = append(m.msgs, "more than 32 `paths:` globs; the rest are ignored")
			return
		case strings.HasPrefix(g, "/") || strings.Contains(g, `\`) || hasDotDot(g):
			m.msgs = append(m.msgs, "`paths:` glob "+quote(g)+" must be relative to the repository root, without \"..\"; ignored")
		case badGlob(g):
			m.msgs = append(m.msgs, "`paths:` glob "+quote(g)+" is malformed; ignored")
		default:
			m.paths = append(m.paths, g)
		}
	}
}

// setAfter reads `after: id-a, id-b`; an entry that is not an id is dropped with a warning.
func (m *itemMeta) setAfter(val string) {
	if m.after != nil {
		m.msgs = append(m.msgs, "more than one `after:`; the first one is used")
		return
	}
	ids := splitList(val)
	if len(ids) == 0 {
		m.msgs = append(m.msgs, "empty `after:` ignored")
		return
	}
	for _, id := range ids {
		switch {
		case len(m.after) >= maxScopeAfter:
			m.msgs = append(m.msgs, "more than 32 `after:` ids; the rest are ignored")
			return
		case len(id) > maxAfterIDLen || !afterID.MatchString(id):
			m.msgs = append(m.msgs, "`after:` entry "+quote(id)+" is not an item id; ignored")
		default:
			m.after = append(m.after, id)
		}
	}
}

func hasDotDot(g string) bool {
	for _, seg := range strings.Split(g, "/") {
		if seg == ".." {
			return true
		}
	}
	return false
}

// badGlob is true when a segment of g is not a valid path.Match pattern.
func badGlob(g string) bool {
	for _, seg := range strings.Split(g, "/") {
		if _, err := path.Match(seg, ""); err != nil {
			return true
		}
	}
	return false
}
