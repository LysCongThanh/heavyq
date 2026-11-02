package preflight

import (
	"testing"
	"time"

	"github.com/LysCongThanh/heavyq/internal/history"
)

func TestShouldNudge(t *testing.T) {
	now := time.Date(2026, 11, 2, 23, 0, 0, 0, time.UTC)
	run := func(ago time.Duration, mod func(*history.Record)) history.Record {
		r := history.Record{End: now.Add(-ago), Lane: "api"}
		if mod != nil {
			mod(&r)
		}
		return r
	}
	five := func() []history.Record {
		var rs []history.Record
		for i := 0; i < NudgeRuns; i++ {
			rs = append(rs, run(time.Duration(i+1)*time.Minute, nil))
		}
		return rs
	}
	cases := []struct {
		name        string
		recs        []history.Record
		items, span bool
		want        bool
	}{
		{"long session, items, no span", five(), true, false, true},
		{"four runs are not long", five()[:4], true, false, false},
		{"no items in the lane", five(), false, false, false},
		{"a run span is open", five(), true, true, false},
		{"runs older than the window", []history.Record{run(31*time.Minute, nil), run(40*time.Minute, nil), run(50*time.Minute, nil), run(time.Hour, nil), run(2*time.Hour, nil)}, true, false, false},
		{"light runs do not count", append(five()[:4:4], run(time.Minute, func(r *history.Record) { r.Light = true })), true, false, false},
		{"cache hits are replays", append(five()[:4:4], run(time.Minute, func(r *history.Record) { r.Cache = "hit" })), true, false, false},
		{"gave up is not work", append(five()[:4:4], run(time.Minute, func(r *history.Record) { r.Result = history.OutcomeGaveUp })), true, false, false},
		{"failed runs are work", append(five()[:4:4], run(time.Minute, func(r *history.Record) { r.Exit = 1 })), true, false, true},
		{"a clock far in the future is not trusted", append(five()[:4:4], run(-time.Hour, nil)), true, false, false},
		{"no records", nil, true, false, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ShouldNudge(c.recs, c.items, c.span, now); got != c.want {
				t.Errorf("ShouldNudge = %v, want %v", got, c.want)
			}
		})
	}
}
