// Package pathscope answers one question for parallel agents: can two path
// globs name a common file? It backs `heavyq item claim`, which refuses to
// hand out an item whose paths overlap a live lease.
//
// The answer is deliberately conservative: when it cannot tell, two globs
// overlap. A false "overlap" costs an agent a wait; a false "disjoint" costs a
// merge conflict, which is what the claim check exists to prevent.
//
// Glob syntax is that of path.Match (`*`, `?`, `[a-z]`) on slash separated,
// repository-relative paths, plus `**` as a whole segment for any number of
// directories (including none). A glob that ends in a literal segment without
// a dot inside it (`flow`, not `a.go`) may be a directory, so it also stands for
// everything below it: `internal/flow` and `internal/flow/` cover
// `internal/flow/a.go`.
package pathscope

import (
	"path"
	"strings"
)

// Intersects reports whether some path can match both globs a and b.
func Intersects(a, b string) bool {
	sa, sb := segments(a), segments(b)
	memo := map[[2]int]bool{}
	var walk func(i, j int) bool
	walk = func(i, j int) bool {
		k := [2]int{i, j}
		if v, ok := memo[k]; ok {
			return v
		}
		v := step(sa, sb, i, j, walk) // every move advances i or j: no cycles
		memo[k] = v
		return v
	}
	return walk(0, 0)
}

// step is one move of the walk over the segments of both globs.
func step(sa, sb []string, i, j int, walk func(i, j int) bool) bool {
	switch {
	case i == len(sa) && j == len(sb):
		return true
	case i < len(sa) && sa[i] == "**":
		// `**` matches no segment, or swallows one segment of the other glob.
		return walk(i+1, j) || (j < len(sb) && walk(i, j+1))
	case j < len(sb) && sb[j] == "**":
		return walk(i, j+1) || (i < len(sa) && walk(i+1, j))
	case i == len(sa) || j == len(sb):
		return false
	}
	return segmentsIntersect(sa[i], sb[j]) && walk(i+1, j+1)
}

// Overlap reports the first pair (one glob of as, one of bs) that intersect.
func Overlap(as, bs []string) (a, b string, ok bool) {
	for _, x := range as {
		for _, y := range bs {
			if Intersects(x, y) {
				return x, y, true
			}
		}
	}
	return "", "", false
}

// segments splits a glob into path segments. Empty and "." segments vanish; a
// last segment that is a literal and does not look like a file name may be a
// directory, so `**` follows it.
func segments(glob string) []string {
	var out []string
	for _, s := range strings.Split(glob, "/") {
		if s != "" && s != "." {
			out = append(out, s)
		}
	}
	if n := len(out); n == 0 || (out[n-1] != "**" && !hasMeta(out[n-1]) && !looksLikeFile(out[n-1])) {
		out = append(out, "**")
	}
	return out
}

// segmentsIntersect reports whether two single-segment patterns can match one
// name. With both holding wildcards it compares only what is certain, the
// literal head and the literal tail, and says "yes" for anything in between.
func segmentsIntersect(a, b string) bool {
	if a == b {
		return true
	}
	la, lb := !hasMeta(a), !hasMeta(b)
	switch {
	case la && lb:
		return false
	case la:
		return matches(b, a)
	case lb:
		return matches(a, b)
	}
	ha, ta := literalEnds(a)
	hb, tb := literalEnds(b)
	return (strings.HasPrefix(ha, hb) || strings.HasPrefix(hb, ha)) &&
		(strings.HasSuffix(ta, tb) || strings.HasSuffix(tb, ta))
}

// looksLikeFile is true for a name with a dot that does not start it: a.go, not .github.
func looksLikeFile(seg string) bool { return strings.Contains(seg[1:], ".") }

func hasMeta(s string) bool { return strings.ContainsAny(s, `*?[\`) }

// literalEnds is the text before the first wildcard and after the last one.
func literalEnds(p string) (head, tail string) {
	i := strings.IndexAny(p, `*?[\`)
	j := strings.LastIndexAny(p, `*?]\`)
	if j < i {
		j = len(p) - 1
	}
	return p[:i], p[j+1:]
}

// matches reports whether pattern matches name. A malformed pattern matches
// everything: the conservative answer.
func matches(pattern, name string) bool {
	ok, err := path.Match(pattern, name)
	return ok || err != nil
}
