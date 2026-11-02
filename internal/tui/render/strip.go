package render

import (
	"strconv"

	"github.com/LysCongThanh/heavyq/internal/snapshot"
)

// strip is the whole state on one line, for a one-row pane or a tmux status:
//
//	hq 2 run, 1 wait, 2 need you | linuxi 5.4G load 16.0/9.0 ! | local 9.6G load 12.3/16.0
func (c *ctx) strip(loaded bool, w int) Line {
	l := Line{c.seg(Bold, "hq")}
	add := func(s Seg) { l = append(l, blanks(1), s) }
	if !loaded {
		add(c.seg(Dim, "..."))
		return l
	}
	s := c.s
	switch {
	case c.v.Err != "":
		add(c.seg(Bad|Bold, "FAILED"))
	case c.v.stale():
		add(c.seg(Warn|Bold, "STALE"))
	}
	if len(s.Running)+len(s.Waiting) == 0 {
		add(c.seg(Good, "idle"))
	} else {
		if n := len(s.Running); n > 0 {
			add(c.seg(Good, strconv.Itoa(n)+" run"))
		}
		if n := len(s.Waiting); n > 0 {
			add(c.seg(Warn, strconv.Itoa(n)+" wait"))
		}
	}
	if n := len(s.Exceptions); n > 0 {
		add(c.seg(Bad|Bold, strconv.Itoa(n)+" need you"))
	} else {
		add(c.seg(Good, "ok"))
	}
	for _, m := range s.Machines {
		add(c.seg(Dim, "|"))
		add(c.seg(Bold, m.Name))
		switch {
		case !m.Reachable:
			add(c.seg(Bad, "down"))
		case m.Paused:
			add(c.seg(Warn, "paused"))
		default:
			add(c.seg(0, size(m.FreeMB)))
			if m.MetricsKnown {
				st := Style(0)
				if m.Admits != "" {
					st = Warn
				}
				add(c.seg(st, "load "+oneDecimal(m.Load1)+"/"+oneDecimal(m.MaxLoad)))
			}
		}
	}
	return l
}

func oneDecimal(f float64) string { return strconv.FormatFloat(f, 'f', 1, 64) }

// Strip renders the one-line strip of a snapshot for scripts and status bars
// (heavyq top --line). Use a Theme with LevelNone for tmux.
func Strip(s snapshot.Snapshot, v View, o Options) string {
	return Frame(&s, ModeLine, v, o)[0]
}
