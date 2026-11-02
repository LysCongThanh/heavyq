// Package notify pushes a local notice when the flow changes in a way a person
// should know about, once per state change and never more than a daily budget:
//
//   - something needs you (a new blocked question or permission prompt),
//   - the forecast turns at risk,
//   - every item is proven.
//
// Evaluate is a pure function of the last notified state and the facts now;
// Check wraps it with the dedupe file under <git common dir>/heavyq/notify/ and
// delivery. Channels are local: a desktop notice (notify-send, osascript) or the
// terminal (OSC 9, OSC 777, bell). The point is reassurance, not alarms.
package notify

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// DefaultBudget is the pushes per local day when HEAVYQ_NOTIFY_BUDGET is not set.
const DefaultBudget = 5

// MaxAge is how old the flow state may be and still push: a stale state is a
// stopped materialiser, not news.
const MaxAge = 10 * time.Minute

// maxLines is how many lines one coalesced message carries.
const maxLines = 3

// Need is one thing waiting for a person; Key identifies it across refreshes.
type Need struct {
	Key  string // item|session|since: the same need keeps the same key
	Text string // one short line, e.g. "doc-7: permission_prompt: Bash(npm run e2e)"
}

// Facts is what the notifier looks at, taken from the flow state.
type Facts struct {
	Now       time.Time
	Updated   time.Time // when the state was materialised
	Needs     []Need
	AtRisk    bool
	AtRiskWhy string
	ETA       time.Time // zero when unknown
	AllProven bool
	Proven    int
	Total     int
}

// State is what the notifier remembers: the dedupe record on disk.
type State struct {
	Day        string   `json:"day"`                  // local date of Sent
	Sent       int      `json:"sent"`                 // pushes today
	Suppressed int      `json:"suppressed,omitempty"` // changes the budget swallowed today
	Needs      []string `json:"needs"`                // keys of the needs already announced and still open
	AtRisk     bool     `json:"at_risk"`
	AllProven  bool     `json:"all_proven"`
}

// Message is one coalesced notice.
type Message struct {
	Title string
	Body  string
}

// Evaluate decides what to push. prev is nil the first time. It returns the
// state to remember and the message to send, nil when there is nothing new or
// the budget is spent. A change the budget swallows is still remembered, so it
// is not pushed later when the budget resets. A stale state changes nothing.
func Evaluate(prev *State, f Facts, budget int) (State, *Message) {
	var next State
	if prev != nil {
		next = *prev
	}
	if f.Now.Sub(f.Updated) > MaxAge {
		return next, nil
	}
	day := f.Now.Format("2006-01-02")
	if next.Day != day {
		next.Day, next.Sent, next.Suppressed = day, 0, 0
	}

	announced := map[string]bool{}
	if prev != nil {
		for _, k := range prev.Needs {
			announced[k] = true
		}
	}
	var lines []string
	next.Needs = next.Needs[:0:0]
	for _, n := range f.Needs {
		next.Needs = append(next.Needs, n.Key)
		if !announced[n.Key] {
			lines = append(lines, "needs you: "+n.Text)
		}
	}
	sort.Strings(next.Needs)

	if f.AtRisk && (prev == nil || !prev.AtRisk) {
		lines = append(lines, atRiskLine(f))
	}
	next.AtRisk = f.AtRisk

	// All proven is news only as a transition seen here: a repository that is
	// already finished when the notifier first looks stays quiet.
	if f.AllProven && prev != nil && !prev.AllProven {
		lines = append(lines, fmt.Sprintf("all %d items proven", f.Total))
	}
	next.AllProven = f.AllProven

	if len(lines) == 0 {
		return next, nil
	}
	if next.Sent >= budget {
		next.Suppressed++
		return next, nil
	}
	next.Sent++
	return next, &Message{Title: "heavyq", Body: coalesce(lines)}
}

func atRiskLine(f Facts) string {
	s := "at risk"
	if f.AtRiskWhy != "" {
		s += ": " + f.AtRiskWhy
	}
	if !f.ETA.IsZero() {
		s += ", ETA ~" + f.ETA.Format("15:04")
	}
	return s
}

// coalesce joins the lines of one evaluation into one message.
func coalesce(lines []string) string {
	if len(lines) > maxLines {
		extra := len(lines) - (maxLines - 1)
		lines = append(lines[:maxLines-1:maxLines-1], fmt.Sprintf("+%d more (heavyq recap)", extra))
	}
	return strings.Join(lines, "\n")
}
