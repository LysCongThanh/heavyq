package anchor

import (
	"regexp"
	"strings"
)

// goalLine matches the first-level heading that carries the goal.
var goalLine = regexp.MustCompile(`(?i)^#[ \t]+goal:[ \t]*(.*)$`)

// walkLines calls fn for every line outside fenced code blocks with its
// 1-based number and parser text (CR and BOM removed). fn returns false to
// stop early.
func walkLines(data []byte, fn func(n int, line string) bool) {
	var fence fenceTracker
	for i, l := range splitLines(data) {
		line, _ := lineText(data, l, i == 0)
		if fence.inCode(line) {
			continue
		}
		if !fn(i+1, line) {
			return
		}
	}
}

// Parse reads an anchor file. path is only recorded (in File.Path and
// Item.Source). It never fails: oddities become Warnings.
//
// Rules: the first "# Goal: text" heading is the goal. Every checkbox line
// outside a code fence is an item; indentation is allowed and each nested
// line is its own item. "id: text" gives the id; a line without a valid id
// prefix gets a slug of its text, with a warning. Repeated ids get "-2",
// "-3", ... and a warning.
func Parse(path string, data []byte) File {
	f := File{Path: path, Items: []Item{}, Warnings: []Warning{}}
	warn := func(line int, msg string) {
		f.Warnings = append(f.Warnings, Warning{Source: path, Line: line, Msg: msg})
	}
	used := map[string]bool{}
	walkLines(data, func(n int, line string) bool {
		if f.GoalLine == 0 {
			if m := goalLine.FindStringSubmatch(line); m != nil {
				f.Goal, f.GoalLine = strings.TrimSpace(m[1]), n
				if f.Goal == "" {
					warn(n, "empty goal")
				}
				return true
			}
		}
		_, checked, raw, ok := matchItemLine(line)
		if !ok {
			return true
		}
		if len(f.Items) >= MaxItems {
			warn(n, "more than 2000 items; the rest are ignored")
			return false
		}
		meta := extractMeta(raw)
		id, text, hasID := splitID(meta.text)
		for _, msg := range meta.msgs {
			warn(n, msg)
		}
		if !hasID {
			if meta.text == "" {
				warn(n, "checkbox without text ignored")
				return true
			}
			id = textSlug(meta.text)
			warn(n, "no valid `id:` prefix (a-z 0-9 . _ -, up to 64 chars); derived id "+quote(id))
		}
		var dup bool
		if id, dup = uniqueID(used, id); dup {
			warn(n, "duplicate id; renamed to "+quote(id))
		}
		f.Items = append(f.Items, Item{
			ID: id, Text: text, Checked: checked, Proof: meta.proof,
			Needs: meta.needs, Source: path, Line: n,
		})
		return true
	})
	if f.GoalLine == 0 {
		warn(0, "no `# Goal:` line")
	}
	return f
}
