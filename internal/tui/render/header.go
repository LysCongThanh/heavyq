package render

import (
	"strconv"

	"github.com/LysCongThanh/heavyq/internal/snapshot"
)

// header is the first line: the product, the queue in one phrase, and on the
// right the clock and how fresh the data is.
func (c *ctx) header(loaded bool, w int) Line {
	left := Line{c.seg(Bold, "heavyq"), blanks(2)}
	if !loaded {
		left = append(left, c.seg(Dim, "waiting for the first snapshot"))
		return c.joinEnds(left, c.freshness(false), w)
	}
	left = append(left, c.summary()...)
	return c.joinEnds(left, c.freshness(true), w)
}

// summary is "2 running · 1 waiting · 4 proven".
func (c *ctx) summary() Line {
	s := c.s
	var parts []Line
	add := func(st Style, n int, word string) {
		parts = append(parts, Line{c.seg(st, strconv.Itoa(n)+" "+word)})
	}
	if len(s.Running)+len(s.Waiting) == 0 {
		parts = append(parts, Line{c.seg(Good, "idle")})
	} else {
		if n := len(s.Running); n > 0 {
			add(Good, n, "running")
		}
		if n := len(s.Waiting); n > 0 {
			add(Warn, n, "waiting")
		}
	}
	proven, unproven := 0, 0
	for _, g := range s.Gates {
		switch g.State {
		case snapshot.GateProven:
			proven++
		case snapshot.GateUnproven:
			unproven++
		}
	}
	if proven > 0 {
		add(0, proven, "proven")
	}
	if unproven > 0 {
		add(Dim, unproven, "unproven")
	}
	var out Line
	for i, p := range parts {
		if i > 0 {
			out = append(out, c.sep())
		}
		out = append(out, p...)
	}
	return out
}

// freshness is the clock and the age of the snapshot; STALE and errors are loud.
func (c *ctx) freshness(loaded bool) Line {
	clock := c.v.Now.Format("15:04:05")
	switch {
	case c.v.Err != "":
		return Line{c.seg(Bad|Bold, "SNAPSHOT FAILED"), blanks(2), c.seg(Dim, clock)}
	case !loaded:
		return Line{c.seg(Dim, clock)}
	case c.v.stale():
		return Line{c.seg(Warn|Bold, "STALE "+dur(c.v.Now.Sub(c.v.Fetched).Seconds())), blanks(2), c.seg(Dim, clock)}
	}
	age := dur(c.v.Now.Sub(c.s.GeneratedAt).Seconds())
	return Line{c.seg(Dim, clock+"  "+age+" ago")}
}

// joinEnds puts left at the start and right at the end of a w-wide line; when
// both do not fit, the clock part of the right goes first.
func (c *ctx) joinEnds(left, right Line, w int) Line {
	if left.Width()+2+right.Width() > w && len(right) > 1 {
		right = right[:1] // keep STALE / FAILED, drop the clock
	}
	gap := w - left.Width() - right.Width()
	if gap < 2 {
		return left
	}
	return append(append(left, blanks(gap)), right...)
}
