package preflight

import (
	"fmt"
	"strings"
)

// CheckDependencies: open `needs:` and blocked items are questions for the
// human now, and every `after:` id must name an item (and not form a cycle). A
// repository whose anchor has no `after:` spans passes the second half: the
// syntax is optional.
func CheckDependencies(items []ItemFact) []Finding {
	var out []Finding
	byID := make(map[string]ItemFact, len(items))
	for _, it := range items {
		byID[it.ID] = it
	}
	for _, it := range items {
		if it.Finished() || !it.Blocked() {
			continue
		}
		need := it.Needs
		if need == "" {
			need = "(no reason recorded)"
		}
		fix := fmt.Sprintf("answer it, then `heavyq item claim %s`", it.ID)
		if it.NeedsInFile {
			fix = fmt.Sprintf("answer it, then delete the `needs:` span at %s", it.Where)
		}
		out = append(out, Finding{Severity: Block, Subject: it.ID,
			Text: fmt.Sprintf("item %s needs a human: %s", it.ID, need), Fix: fix})
	}
	for _, it := range items {
		if it.Finished() {
			continue
		}
		for _, dep := range it.After {
			if _, ok := byID[dep]; !ok {
				out = append(out, Finding{Severity: Block, Subject: it.ID,
					Text: fmt.Sprintf("item %s waits on %q, which is not an item", it.ID, dep),
					Fix:  fmt.Sprintf("edit %s: fix the `after:` id of %s", it.Where, it.ID)})
			}
		}
	}
	for _, cycle := range cycles(items, byID) {
		out = append(out, Finding{Severity: Block, Subject: cycle[0],
			Text: "items wait on each other: " + strings.Join(append(cycle, cycle[0]), " -> "),
			Fix:  fmt.Sprintf("edit %s: break the `after:` loop", byID[cycle[0]].Where)})
	}
	return out
}

// cycles finds the `after:` loops, each reported once, as the ids along it. Ids
// that are not items are ignored (the caller reports them).
func cycles(items []ItemFact, byID map[string]ItemFact) [][]string {
	const (
		fresh = iota
		onPath
		done
	)
	state := map[string]int{}
	var path []string
	var found [][]string
	var visit func(id string)
	visit = func(id string) {
		state[id] = onPath
		path = append(path, id)
		for _, dep := range byID[id].After {
			if _, ok := byID[dep]; !ok {
				continue
			}
			switch state[dep] {
			case fresh:
				visit(dep)
			case onPath:
				for i, p := range path {
					if p == dep {
						found = append(found, append([]string(nil), path[i:]...))
						break
					}
				}
			}
		}
		path = path[:len(path)-1]
		state[id] = done
	}
	for _, it := range items {
		if state[it.ID] == fresh {
			visit(it.ID)
		}
	}
	return found
}

// ParseAfter returns the ids of the `after:` code span of an anchor line, in
// order: `after: a, b` or `after: a b`. A line without one, or with an empty
// one, has none. The anchor parser keeps other spans out of the item text, so
// preflight reads this one from the raw line.
func ParseAfter(line string) []string {
	for rest := line; ; {
		open := strings.IndexByte(rest, '`')
		if open < 0 {
			return nil
		}
		end := strings.IndexByte(rest[open+1:], '`')
		if end < 0 {
			return nil
		}
		span := strings.TrimSpace(rest[open+1 : open+1+end])
		rest = rest[open+end+2:]
		if v, ok := cutPrefixFold(span, "after:"); ok {
			return strings.FieldsFunc(v, func(r rune) bool { return r == ',' || r == ' ' || r == '\t' })
		}
	}
}

func cutPrefixFold(s, prefix string) (string, bool) {
	if len(s) >= len(prefix) && strings.EqualFold(s[:len(prefix)], prefix) {
		return s[len(prefix):], true
	}
	return s, false
}
