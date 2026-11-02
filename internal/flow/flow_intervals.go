package flow

import (
	"sort"
	"time"
)

// paint turns overlapping candidate spans into one non-overlapping sequence:
// at every instant the candidate of the highest priority wins, and adjacent
// pieces of the same state merge. Empty spans are dropped.
func paint(cands []Span) []Span {
	var cuts []time.Time
	live := cands[:0:0]
	for _, c := range cands {
		if !c.End.After(c.Start) {
			continue
		}
		live = append(live, c)
		cuts = append(cuts, c.Start, c.End)
	}
	sort.Slice(cuts, func(i, j int) bool { return cuts[i].Before(cuts[j]) })
	var out []Span
	for i := 0; i+1 < len(cuts); i++ {
		a, b := cuts[i], cuts[i+1]
		if !b.After(a) {
			continue
		}
		best := State("")
		for _, c := range live {
			if !c.Start.After(a) && !c.End.Before(b) && priority[c.State] > priority[best] {
				best = c.State
			}
		}
		if best == "" {
			continue // a gap: nobody was doing anything the sources can see
		}
		if n := len(out); n > 0 && out[n-1].State == best && out[n-1].End.Equal(a) {
			out[n-1].End = b
			continue
		}
		out = append(out, Span{State: best, Start: a, End: b})
	}
	return out
}

// interval is a half-open time range.
type interval struct{ start, end time.Time }

func (iv interval) empty() bool { return !iv.end.After(iv.start) }

// intersect is the overlap of two intervals (possibly empty).
func intersect(a, b interval) interval {
	s, e := a.start, a.end
	if b.start.After(s) {
		s = b.start
	}
	if b.end.Before(e) {
		e = b.end
	}
	return interval{s, e}
}

// subtract removes every interval of holes from iv and returns what is left.
func subtract(iv interval, holes []interval) []interval {
	rest := []interval{iv}
	for _, h := range holes {
		var next []interval
		for _, r := range rest {
			x := intersect(r, h)
			if x.empty() {
				next = append(next, r)
				continue
			}
			if x.start.After(r.start) {
				next = append(next, interval{r.start, x.start})
			}
			if r.end.After(x.end) {
				next = append(next, interval{x.end, r.end})
			}
		}
		rest = next
	}
	return rest
}

// clip cuts spans to [from, to) and drops the empty ones.
func clip(spans []Span, from, to time.Time) []Span {
	var out []Span
	for _, s := range spans {
		if s.Start.Before(from) {
			s.Start = from
		}
		if s.End.After(to) {
			s.End = to
		}
		if s.End.After(s.Start) {
			out = append(out, s)
		}
	}
	return out
}

// unionSeconds is the wall-clock length of the union of spans.
func unionSeconds(spans []Span) float64 {
	s := append([]Span(nil), spans...)
	sortSpans(s)
	var total time.Duration
	var curEnd time.Time
	for _, sp := range s {
		start := sp.Start
		if start.Before(curEnd) {
			start = curEnd
		}
		if sp.End.After(start) {
			total += sp.End.Sub(start)
			curEnd = sp.End
		}
	}
	return total.Seconds()
}
