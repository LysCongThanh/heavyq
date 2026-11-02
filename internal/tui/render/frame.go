package render

import (
	"strconv"
	"strings"

	"github.com/LysCongThanh/heavyq/internal/snapshot"
)

// Frame draws the screen as lines of terminal text, each ending with an SGR
// reset. s is nil before the first snapshot arrived. Every line is at most
// o.Cols cells wide. With o.Rows > 0 there are at most that many lines and
// the footer is the last one; the body scrolls to keep the selection visible.
func Frame(s *snapshot.Snapshot, mode Mode, v View, o Options) []string {
	if o.Cols < minCols {
		return []string{Line{{"heavyq: window too narrow", 0}}.Fit(max(o.Cols, 1), "").Render(o.Theme.level())}
	}
	var snap snapshot.Snapshot
	if s != nil {
		snap = *s
	}
	c := newCtx(snap, v, o.Theme)
	ell := o.Theme.g(gEll)
	if mode == ModeLine {
		return []string{c.strip(s != nil, o.Cols).Fit(o.Cols, ell).Render(o.Theme.level())}
	}
	head := c.header(s != nil, o.Cols)
	foot := c.footer(mode, o.Cols)
	var body []bodyLine
	switch {
	case v.Overlay == OverlayHelp:
		body = c.helpLines(o.Cols)
	case v.Overlay == OverlayWhy:
		body = c.whyLines(o.Cols)
	case s != nil:
		body = c.body(mode, o.Cols)
	}
	if o.Rows > 0 {
		body = scroll(body, v.Selected, o.Rows-2, c)
	}
	lines := []Line{head}
	for _, b := range body {
		lines = append(lines, b.Line)
	}
	if o.Rows > 0 { // pin the footer to the last row
		for len(lines) < o.Rows-1 {
			lines = append(lines, nil)
		}
	}
	lines = append(lines, foot)
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = l.Fit(o.Cols, ell).Render(o.Theme.level())
	}
	return out
}

func (t Theme) level() Level { return t.Level }

// body is the sections of the mode, in one column or two.
func (c *ctx) body(mode Mode, w int) []bodyLine {
	compact := w < compactBelow
	rows := RowsIn(c.s, c.v.Filter, mode)
	switch mode {
	case ModeFocus:
		return c.exceptionSection(rows, w, compact)
	case ModeLanes:
		cols := colsTable
		if compact {
			cols = colsCompact
		}
		return c.laneSection(rows, w, cols, true)
	case ModeMachines:
		out := c.machineSection(rows, w, true)
		out = append(out, c.poolSection(w, true)...)
		return append(out, c.outsideSection(w)...)
	}
	cols := colsFull
	if compact {
		cols = colsCompact
	}
	if w >= wideCols {
		return c.wideBody(rows, w, cols)
	}
	out := c.exceptionSection(rows, w, compact)
	out = append(out, c.laneSection(rows, w, cols, false)...)
	out = append(out, c.machineSection(rows, w, false)...)
	out = append(out, c.poolSection(w, false)...)
	return append(out, c.storySection(w, map[bool]int{true: 2, false: 3}[compact])...)
}

// wideBody puts lanes on the left and machines, pools and the story on the right.
func (c *ctx) wideBody(rows []Row, w int, cols []colID) []bodyLine {
	lw := w - sideWidth - 3
	left := c.exceptionSection(rows, lw, false)
	left = append(left, c.laneSection(rows, lw, cols, false)...)
	right := c.machineSection(rows, sideWidth, false)
	right = append(right, c.poolSection(sideWidth, false)...)
	right = append(right, c.storySection(sideWidth, 8)...)
	ell := c.th.g(gEll)
	n := max(len(left), len(right))
	out := make([]bodyLine, n)
	for i := range out {
		var l, r bodyLine
		if i < len(left) {
			l = left[i]
		}
		if i < len(right) {
			r = right[i]
		}
		line := append(fitted(l.Line, lw, ell), Seg{" | ", Dim})
		line = append(line, r.Line...)
		out[i] = bodyLine{Line: line, Key: l.Key + "\x00" + r.Key}
	}
	return out
}

// scroll shows height body lines, keeping the selected row in view, with a
// marker line where rows are hidden.
func scroll(body []bodyLine, selected string, height int, c *ctx) []bodyLine {
	if height < 1 || len(body) <= height {
		return body
	}
	selAt := 0
	for i, b := range body {
		if selected != "" && keyMatches(b.Key, selected) {
			selAt = i
		}
	}
	off := max(0, selAt-height+2) // the selection sits near the bottom once past the first page
	end := min(len(body), off+height)
	view := append([]bodyLine(nil), body[off:end]...)
	if off > 0 {
		view[0] = c.plainLine(c.mark(gLess, Dim), c.seg(Dim, strconv.Itoa(off)+" more above"))
	}
	if end < len(body) {
		view[len(view)-1] = c.plainLine(c.mark(gMore, Dim), c.seg(Dim, strconv.Itoa(len(body)-end+1)+" more below"))
	}
	return view
}

// keyMatches reports whether a body line carries the row key (a wide line carries two, NUL separated).
func keyMatches(lineKey, key string) bool {
	for _, k := range strings.Split(lineKey, "\x00") {
		if k == key {
			return true
		}
	}
	return false
}
