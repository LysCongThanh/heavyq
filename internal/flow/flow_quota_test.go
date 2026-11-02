package flow

import (
	"strings"
	"testing"
	"time"

	"github.com/LysCongThanh/heavyq/internal/history"
	"github.com/LysCongThanh/heavyq/internal/lease"
)

// stop is a turn that ended on the usage limit.
func stop(min int, lane, session string) Event {
	return Event{At: at(min), Kind: EventStopFailure, Lane: lane, Session: session, Class: ClassStopFailure, Detail: "rate_limit", Reason: "stop_failure: rate_limit"}
}

func fourStops() []Event {
	return []Event{stop(0, "heavyq", "s0"), stop(1, "agent-a", "s1"), stop(2, "agent-b", "s2"), stop(7, "agent-c", "s3")}
}

func pct(v float64) *float64 { return &v }

func TestQuotaStopsAreOneFleetEntryNotOnePerLane(t *testing.T) {
	reset := at(300)
	in := Input{Now: at(30), Hooks: fourStops(), Session: &SessionInfo{Updated: at(25), RateLimits: &RateLimits{FiveHourPct: pct(100), FiveHourResetsAt: &reset}}}
	snap := Build(in)
	q := snap.Quota
	if q == nil || q.Agents != 4 || !q.Since.Equal(at(0)) || !q.LastHit.Equal(at(7)) || q.Window != "five_hour" || q.ResetsAt == nil || !q.ResetsAt.Equal(reset) {
		t.Fatalf("quota: %+v", q)
	}
	if len(snap.NeedsYou) != 1 || snap.NeedsYou[0].Lane != "fleet" || !snap.NeedsYou[0].Since.Equal(at(0)) {
		t.Fatalf("one fleet entry, no per-lane ones: %+v", snap.NeedsYou)
	}
	if got := snap.NeedsYou[0].Reason; got != "quota hit at 10:00, 4 agents stopped, resets ~15:00" {
		t.Errorf("reason: %q", got)
	}
	for _, l := range snap.Lanes {
		if l.State == BlockedHuman {
			t.Errorf("a usage-limit stop is not a person being waited on: %+v", l)
		}
	}
	if len(snap.Events) != 4 {
		t.Errorf("the stops stay visible as events: %+v", snap.Events)
	}
	if got := QuotaText(&QuotaInfo{Since: at(0), Agents: 1}, nil); got != "quota hit at 10:00, 1 agent stopped" {
		t.Errorf("no reset known: %q", got)
	}
}

func TestQuotaClearsOnActivityOfAnySessionAfterTheGrace(t *testing.T) {
	run := func(min int, lane, session string) history.Record {
		return history.Record{End: at(min), Lane: lane, Session: session, RunSecs: 30, Cmd: "go test"}
	}
	cases := map[string]struct {
		in   Input
		open bool
	}{
		"nothing happened":              {Input{Now: at(60), Hooks: fourStops()}, true},
		"a run inside the grace":        {Input{Now: at(60), Hooks: fourStops(), Records: []history.Record{run(8, "agent-d", "s9")}}, true},
		"a run of an unrelated session": {Input{Now: at(60), Hooks: fourStops(), Records: []history.Record{run(50, "agent-d", "s9")}}, false},
		"a lease taken":                 {Input{Now: at(60), Hooks: fourStops(), Leases: []lease.Event{{At: at(40), Op: lease.OpClaim, Item: "x", Lane: "agent-d"}}}, false},
		"a session's next tool call":    {Input{Now: at(60), Hooks: append(fourStops(), Event{At: at(45), Kind: EventCleared, Lane: "heavyq", Session: "s0"})}, false},
		"a cleared event inside grace":  {Input{Now: at(60), Hooks: append(fourStops(), Event{At: at(8), Kind: EventCleared, Lane: "heavyq", Session: "s0"})}, true},
		"the fleet-level clear":         {Input{Now: at(60), Hooks: append(fourStops(), Event{At: at(45), Kind: EventQuotaCleared})}, false},
		"a clear older than the stop":   {Input{Now: at(60), Hooks: append([]Event{{At: at(-5), Kind: EventQuotaCleared}}, fourStops()...)}, true},
		"nobody answered for a day":     {Input{Now: at(60 + 25*60), Hooks: fourStops()}, false},
	}
	for name, c := range cases {
		if got := Build(c.in).Quota != nil; got != c.open {
			t.Errorf("%s: quota open = %v, want %v", name, got, c.open)
		}
	}
}

func TestQuotaClearsWhenTheStatusLineShowsTheWindowReset(t *testing.T) {
	past, soon := at(40), at(300)
	cases := map[string]struct {
		s    *SessionInfo
		open bool
	}{
		"still full, resets later":                     {&SessionInfo{Updated: at(25), RateLimits: &RateLimits{FiveHourPct: pct(100), FiveHourResetsAt: &soon}}, true},
		"the reset time is past":                       {&SessionInfo{Updated: at(25), RateLimits: &RateLimits{FiveHourPct: pct(100), FiveHourResetsAt: &past}}, false},
		"a reset from before the stop is not this one": {&SessionInfo{Updated: at(-30), RateLimits: &RateLimits{FiveHourResetsAt: func() *time.Time { t := at(-20); return &t }()}}, true},
		"headroom reported after the grace":            {&SessionInfo{Updated: at(50), RateLimits: &RateLimits{FiveHourPct: pct(3), SevenDayPct: pct(40)}}, false},
		"headroom reported inside the grace":           {&SessionInfo{Updated: at(8), RateLimits: &RateLimits{FiveHourPct: pct(3)}}, true},
		"weekly window still full":                     {&SessionInfo{Updated: at(50), RateLimits: &RateLimits{FiveHourPct: pct(3), SevenDayPct: pct(100)}}, true},
		"nothing known":                                {&SessionInfo{Updated: at(50)}, true},
	}
	for name, c := range cases {
		if got := Build(Input{Now: at(60), Hooks: fourStops(), Session: c.s}).Quota != nil; got != c.open {
			t.Errorf("%s: quota open = %v, want %v", name, got, c.open)
		}
	}
	if w, r := quotaWindow(&SessionInfo{RateLimits: &RateLimits{FiveHourPct: pct(10), SevenDayPct: pct(100), SevenDayResetsAt: &soon}}); w != "seven_day" || r == nil {
		t.Errorf("only the weekly window is full: %v %v", w, r)
	}
}

func TestQuotaEpisodesAreSeparatedByActivityAndByNotifications(t *testing.T) {
	hooks := []Event{stop(0, "a", "s1"), stop(1, "b", "s2"),
		{At: at(100), Kind: EventQuota, Lane: "a", Session: "s1", Detail: "quota_auto_resume_scheduled"}}
	recs := []history.Record{{End: at(60), Lane: "a", Session: "s1", RunSecs: 10}} // ran again between the two
	q := Build(Input{Now: at(110), Hooks: hooks, Records: recs}).Quota
	if q == nil || q.Agents != 1 || !q.Since.Equal(at(100)) {
		t.Fatalf("the second episode is its own: %+v", q)
	}
}

func TestBlockClearsWhenTheSessionShowsLifeUnderAnotherLane(t *testing.T) {
	blocked := Event{At: at(0), Kind: EventBlocked, Lane: "heavyq", Session: "s1", Class: ClassPermissionRequest, Reason: "permission_request: Bash(x)"}
	cases := map[string]struct {
		hooks []Event
		recs  []history.Record
		open  bool
	}{
		"nothing since": {[]Event{blocked}, nil, true},
		"the same session's next tool call, recorded under another lane": {[]Event{blocked, {At: at(5), Kind: EventCleared, Lane: "agent-x", Session: "s1"}}, nil, false},
		"another session's tool call changes nothing":                    {[]Event{blocked, {At: at(5), Kind: EventCleared, Lane: "agent-x", Session: "s2"}}, nil, true},
		"a run of the same session in another lane":                      {[]Event{blocked}, []history.Record{{End: at(9), Lane: "agent-x", Session: "s1", RunSecs: 60}}, false},
		"a run of the lane under another session":                        {[]Event{blocked}, []history.Record{{End: at(9), Lane: "heavyq", Session: "s7", RunSecs: 60}}, false},
		"a run of someone else":                                          {[]Event{blocked}, []history.Record{{End: at(9), Lane: "other", Session: "s7", RunSecs: 60}}, true},
	}
	for name, c := range cases {
		snap := Build(Input{Now: at(20), Hooks: c.hooks, Records: c.recs})
		if got := len(snap.NeedsYou) == 1; got != c.open {
			t.Errorf("%s: needs-you open = %v, want %v (%+v)", name, got, c.open, snap.NeedsYou)
		}
	}
}

func TestLanesOfRemovedWorktreesDropOut(t *testing.T) {
	hooks := []Event{
		{At: at(0), Kind: EventBlocked, Lane: "agent-gone", Session: "s1", Class: ClassIdlePrompt, Reason: "idle_prompt"},
		{At: at(1), Kind: EventBlocked, Lane: "agent-here", Session: "s2", Class: ClassIdlePrompt, Reason: "idle_prompt"},
	}
	recs := []history.Record{{End: at(8), Lane: "agent-gone", Session: "s9", RunSecs: 30}, {End: at(9), Lane: "agent-here2", Session: "s8", RunSecs: 30}}
	in := Input{Now: at(12), Hooks: hooks, Records: recs[1:]}
	if snap := Build(in); len(snap.NeedsYou) != 2 {
		t.Fatalf("both wait: %+v", snap.NeedsYou)
	}
	in.GoneLanes = map[string]bool{"agent-gone": true}
	snap := Build(in)
	if len(snap.NeedsYou) != 1 || snap.NeedsYou[0].Lane != "agent-here" {
		t.Errorf("needs: %+v", snap.NeedsYou)
	}
	in.Records = recs
	in.GoneLanes["agent-here2"] = true
	snap = Build(in)
	for _, l := range snap.Lanes {
		if strings.Contains(l.Lane, "gone") || l.Lane == "agent-here2" {
			t.Errorf("a lane of a removed work tree is not active: %+v", l)
		}
	}
}
