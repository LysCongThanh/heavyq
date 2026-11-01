package anchor

import "strconv"

// ParseExtra reads the checkboxes of a plan or docs file as read-only items.
// path is the repo-relative slash path; it is recorded in Item.Source and
// feeds the ids.
//
// The id is "<file-slug>/<n>-<text-slug>": n counts the items of this file
// with the same text slug, from 1, in file order. It does not depend on line
// numbers, so inserting or moving unrelated lines never renames an item;
// changing an item's text (or adding an identical one above it) does. No
// "id:" prefix is read: the whole text, minus `proof:` / `needs:` spans, is
// the text. An inline `proof:` is honoured.
func ParseExtra(path string, data []byte) ([]Item, []Warning) {
	items := []Item{}
	warns := []Warning{}
	warn := func(line int, msg string) {
		warns = append(warns, Warning{Source: path, Line: line, Msg: msg})
	}
	prefix := fileSlug(path)
	seen := map[string]int{}
	walkLines(data, func(n int, line string) bool {
		_, checked, raw, ok := matchItemLine(line)
		if !ok {
			return true
		}
		if len(items) >= MaxItems {
			warn(n, "more than 2000 items; the rest are ignored")
			return false
		}
		meta := extractMeta(raw)
		for _, msg := range meta.msgs {
			warn(n, msg)
		}
		if meta.text == "" {
			warn(n, "checkbox without text ignored")
			return true
		}
		ts := textSlug(meta.text)
		seen[ts]++
		items = append(items, Item{
			ID:      prefix + "/" + strconv.Itoa(seen[ts]) + "-" + ts,
			Text:    meta.text,
			Checked: checked,
			Proof:   meta.proof,
			Needs:   meta.needs,
			Source:  path, Line: n, ReadOnly: true,
		})
		return true
	})
	return items, warns
}
