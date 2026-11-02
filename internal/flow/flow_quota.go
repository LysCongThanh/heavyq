package flow

import (
	"fmt"
	"sort"
	"time"
)

// QuotaGrace is how long after the last stop on a usage limit activity does not
// count as the quota being back: agents mid-turn when the limit hit keep
// finishing tool calls for a moment, and the stops of a fleet arrive minutes
// apart.
const QuotaGrace = 2 * time.Minute

// QuotaInfo is the one fleet-level entry a usage-limit stop becomes: the
// account's quota ran out, so every agent stops, but that is one fact, not one
// needs-you entry per lane. It is the governor's input (stagger, backoff).
type QuotaInfo struct {
	Since   time.Time `json:"since"`    // the first stop of this episode
	LastHit time.Time `json:"last_hit"` // the latest one
	Agents  int       `json:"agents"`   // lanes (else sessions) stopped
	Lanes   []string  `json:"lanes"`
	// Window is "five_hour" or "seven_day" when the status line says which one is
	// used up (or, with neither at 100%, the short one); ResetsAt is when it resets,
	// null when the status line never said.
	Window   string     `json:"window,omitempty"`
	ResetsAt *time.Time `json:"resets_at"`
}

// isQuotaStop: the event says the account's usage limit stopped an agent: a
// turn that ended on rate_limit, or a quota notification.
func isQuotaStop(e Event) bool {
	return (e.Kind == EventStopFailure && e.Detail == "rate_limit") || e.Kind == EventQuota
}

// deriveQuota folds the hook events into the open quota episode, nil when there
// is none. An episode opens with the first stop on the limit and closes when
//
//   - a fleet-level quota_cleared event follows the last stop (the PostToolUse
//     hook of any session wrote it),
//   - some session showed activity past QuotaGrace after the last stop: a ledger
//     run or lease began, or a session's next tool call cleared its block,
//   - the status line says the window reset: its reset time fell after the first
//     stop and is past, or its newest report, made after the grace, has every
//     window under 100%,
//   - or nobody answered for blockMaxAge (the sessions are gone).
func deriveQuota(in Input) *QuotaInfo {
	evs := append([]Event(nil), in.Hooks...)
	sort.SliceStable(evs, func(i, j int) bool { return evs[i].At.Before(evs[j].At) })
	acts := activitiesOf(in.Records, in.Leases)
	var q *QuotaInfo
	lanes := map[string]bool{}
	for _, e := range evs {
		switch {
		case isQuotaStop(e):
			if q != nil && wentOn(acts, evs, q.LastHit.Add(QuotaGrace), e.At) {
				q = nil // the agents ran in between: this is a new episode
			}
			if q == nil {
				q, lanes = &QuotaInfo{Since: e.At}, map[string]bool{}
			}
			q.LastHit = e.At
			if who := firstNonEmpty(e.Lane, e.Session); who != "" {
				lanes[who] = true
			}
		case e.Kind == EventQuotaCleared && q != nil && !e.At.Before(q.LastHit):
			q = nil
		}
	}
	if q == nil || in.Now.Sub(q.LastHit) > blockMaxAge {
		return nil
	}
	settled := q.LastHit.Add(QuotaGrace)
	if wentOn(acts, evs, settled, in.Now.Add(time.Hour)) {
		return nil
	}
	q.Window, q.ResetsAt = quotaWindow(in.Session)
	if q.ResetsAt != nil && q.ResetsAt.After(q.Since) && !q.ResetsAt.After(in.Now) {
		return nil // the window reset after we were stopped
	}
	if allUnderLimit(in.Session) && !in.Session.Updated.Before(settled) {
		return nil // the status line reports headroom again
	}
	for l := range lanes {
		q.Lanes = append(q.Lanes, l)
	}
	sort.Strings(q.Lanes)
	q.Agents = len(q.Lanes)
	return q
}

// wentOn: in [from, to) some session did something: a ledger run or a lease
// began, or a session's next tool call cleared its block.
func wentOn(acts []activity, evs []Event, from, to time.Time) bool {
	for _, a := range acts {
		if !a.Start.Before(from) && a.Start.Before(to) {
			return true
		}
	}
	for _, e := range evs {
		if e.Kind == EventCleared && !e.At.Before(from) && e.At.Before(to) {
			return true
		}
	}
	return false
}

// quotaWindow is the window the status line says is used up and when it
// resets: the weekly one only when it alone is at 100%, else the five-hour one.
func quotaWindow(s *SessionInfo) (string, *time.Time) {
	if s == nil || s.RateLimits == nil {
		return "", nil
	}
	rl := s.RateLimits
	full := func(p *float64) bool { return p != nil && *p >= 100 }
	if full(rl.SevenDayPct) && !full(rl.FiveHourPct) {
		return "seven_day", rl.SevenDayResetsAt
	}
	if rl.FiveHourPct == nil && rl.FiveHourResetsAt == nil {
		return "", nil
	}
	return "five_hour", rl.FiveHourResetsAt
}

// allUnderLimit: the status line knows at least one window and none is full.
func allUnderLimit(s *SessionInfo) bool {
	if s == nil || s.RateLimits == nil {
		return false
	}
	known := false
	for _, p := range []*float64{s.RateLimits.FiveHourPct, s.RateLimits.SevenDayPct} {
		if p == nil {
			continue
		}
		if *p >= 100 {
			return false
		}
		known = true
	}
	return known
}

// QuotaText is the entry as one line: "quota hit at 19:20, 4 agents stopped,
// resets ~23:20", times in loc.
func QuotaText(q *QuotaInfo, loc *time.Location) string {
	if loc == nil {
		loc = time.UTC
	}
	agents := "1 agent"
	if q.Agents != 1 {
		agents = fmt.Sprintf("%d agents", q.Agents)
	}
	s := fmt.Sprintf("quota hit at %s, %s stopped", q.Since.In(loc).Format("15:04"), agents)
	if q.ResetsAt != nil {
		s += ", resets ~" + q.ResetsAt.In(loc).Format("15:04")
	}
	return s
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
