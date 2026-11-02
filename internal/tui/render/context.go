package render

import (
	"strings"
	"time"

	"github.com/LysCongThanh/heavyq/internal/snapshot"
	"github.com/LysCongThanh/heavyq/internal/tui/width"
)

// Overlay is a full-body panel that replaces the sections.
type Overlay int

// Overlays.
const (
	OverlayNone Overlay = iota
	OverlayHelp
	OverlayWhy
)

// View is everything about the screen that is not the snapshot: what is
// selected, typed, asked or flashed. Now is passed in so Frame stays pure.
type View struct {
	Now      time.Time
	Interval time.Duration // snapshot period; the screen is STALE after three of them
	Fetched  time.Time     // when the snapshot arrived; zero before the first one
	Err      string        // the last snapshot build failed with this
	Selected string        // Row.Key
	Filter   string
	Typing   bool // the filter is being typed
	Overlay  Overlay
	WhyTitle string
	WhyLines []string
	Prompt   string // an inline question, e.g. "cancel api? y/n"
	Flash    string // a one-line result of the last action
	Control  bool   // actions are enabled (--control)
}

// Options is the size and the theme. Rows 0 means "as tall as needed" (the
// footer follows the body instead of sitting on the last row).
type Options struct {
	Cols, Rows int
	Theme      Theme
}

// Layout thresholds in columns.
const (
	wideCols     = 140 // a right column appears
	compactBelow = 60  // below this the rows collapse
	minCols      = 24  // below this there is only a message
	sideWidth    = 52  // the right column's width in wide layout
)

// staleAfter is how many snapshot intervals without news make the screen STALE.
const staleAfter = 3

// stale reports whether the data is older than staleAfter intervals.
func (v View) stale() bool {
	if v.Fetched.IsZero() || v.Interval <= 0 {
		return false
	}
	return v.Now.Sub(v.Fetched) > staleAfter*v.Interval
}

// bodyLine is a line of the body; Key is the Row it belongs to, if any.
type bodyLine struct {
	Line Line
	Key  string
}

// ctx carries what every section builder needs.
type ctx struct {
	th      Theme
	v       View
	s       snapshot.Snapshot
	elapsed float64 // seconds between the snapshot and Now: live counters add it
}

func newCtx(s snapshot.Snapshot, v View, th Theme) *ctx {
	c := &ctx{th: th, v: v, s: s}
	if !s.GeneratedAt.IsZero() && v.Now.After(s.GeneratedAt) {
		c.elapsed = v.Now.Sub(s.GeneratedAt).Seconds()
	}
	return c
}

// seg is text in a style, made safe for the theme.
func (c *ctx) seg(st Style, text string) Seg { return Seg{c.th.txt(text), st} }

// mark is a status glyph and the space after it: text never touches a glyph.
func (c *ctx) mark(g glyph, st Style) Seg { return Seg{c.th.g(g) + " ", st} }

// sep separates facts in a phrase.
func (c *ctx) sep() Seg { return Seg{", ", Dim} }

func blanks(n int) Seg {
	if n < 0 {
		n = 0
	}
	return Seg{strings.Repeat(" ", n), 0}
}

// heading is a section title followed by a rule to the edge.
func (c *ctx) heading(title string, st Style, w int) Line {
	l := Line{c.seg(Bold|st, title)}
	if rest := w - l.Width() - 1; rest > 0 {
		l = append(l, blanks(1), Seg{strings.Repeat(c.th.g(gRule), rest), Dim})
	}
	return l
}

// row is a selectable line: a gutter (the marker when selected), the content,
// and for the selected row a reverse-video highlight out to w.
func (c *ctx) row(key string, content Line, w int) bodyLine {
	sel := key != "" && key == c.v.Selected
	gutter := blanks(2)
	if sel {
		gutter = Seg{c.th.g(gSel) + " ", Bold}
	}
	l := append(Line{gutter}, content...)
	if sel {
		l = l.Fit(w, c.th.g(gEll))
		l = append(l, blanks(w-l.Width()))
		l = l.With(Rev)
	}
	return bodyLine{Line: l, Key: key}
}

// plainLine is a non-selectable body line indented under its heading.
func (c *ctx) plainLine(content ...Seg) bodyLine {
	return bodyLine{Line: append(Line{blanks(2)}, content...)}
}

// fitted truncates to w and pads to exactly w cells (for joining columns).
func fitted(l Line, w int, ell string) Line {
	l = l.Fit(w, ell)
	return append(l, blanks(w-l.Width()))
}

// cellWidth is the display width, spelled out for call sites.
func cellWidth(s string) int { return width.String(s) }
