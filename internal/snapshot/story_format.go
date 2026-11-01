package snapshot

import (
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// plural is "1 job", "2 jobs".
func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return strconv.Itoa(n) + " " + word + "s"
}

// dur formats seconds the way the dashboard does: 45s, 3m 05s, 2h 04m.
func dur(secs float64) string {
	s := int(math.Round(max(0, secs)))
	switch {
	case s < 60:
		return fmt.Sprintf("%ds", s)
	case s < 3600:
		if r := s % 60; r != 0 {
			return fmt.Sprintf("%dm %02ds", s/60, r)
		}
		return fmt.Sprintf("%dm", s/60)
	}
	return fmt.Sprintf("%dh %02dm", s/3600, s%3600/60)
}

// mb formats megabytes: 640 MB, 3.2 GB, 24 GB.
func mb(v int) string {
	m := float64(max(0, v))
	switch {
	case m >= 10240:
		return strconv.FormatFloat(m/1024, 'f', 0, 64) + " GB"
	case m >= 1024:
		return strconv.FormatFloat(m/1024, 'f', 1, 64) + " GB"
	}
	return strconv.Itoa(int(math.Round(m))) + " MB"
}

// windowLabel names an insights window: 30m, 1h, 24h, 7d.
func windowLabel(d time.Duration) string {
	switch {
	case d >= 24*time.Hour && d%(24*time.Hour) == 0:
		return strconv.Itoa(int(d/(24*time.Hour))) + "d"
	case d >= time.Hour && d%time.Hour == 0:
		return strconv.Itoa(int(d/time.Hour)) + "h"
	}
	return strconv.Itoa(int(d.Round(time.Minute)/time.Minute)) + "m"
}

// ramShortRe matches the resource gate's RAM reasons (schedule.Gate):
//
//	RAM 1200 MB < 3000 MB needed (job 2500 + reserve 500)
//	RAM 4000 MB (minus 2500 MB held for just-started jobs) < 3000 MB needed
var ramShortRe = regexp.MustCompile(`^RAM (\d+) MB(?: \(minus (\d+) MB held[^)]*\))? < (\d+) MB needed`)

// ramShort returns how many MB a RAM reason lacks, or false when reason is
// not a RAM reason.
func ramShort(reason string) (int, bool) {
	m := ramShortRe.FindStringSubmatch(reason)
	if m == nil {
		return 0, false
	}
	free, _ := strconv.Atoi(m[1])
	held, _ := strconv.Atoi(m[2]) // "" when absent
	need, _ := strconv.Atoi(m[3])
	return max(0, need-(free-held)), true
}

type machineReason struct{ machine, reason string }

// sortedReasons lists a waiting ticket's per-machine reasons by machine
// name, the order the JSON snapshot carries them in.
func sortedReasons(reasons map[string]string) []machineReason {
	out := make([]machineReason, 0, len(reasons))
	for m, r := range reasons {
		out = append(out, machineReason{m, r})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].machine < out[j].machine })
	return out
}

// startsNow reports whether any machine would take the ticket on its next
// poll (an empty reason).
func startsNow(reasons []machineReason) bool {
	for _, r := range reasons {
		if r.reason == "" {
			return true
		}
	}
	return false
}

// allBlocked is true when no machine would take the ticket now.
func allBlocked(reasons map[string]string) bool {
	for _, r := range reasons {
		if r == "" {
			return false
		}
	}
	return true
}

// isBehind reports a "behind lane (p3)" reason: queue order, not a resource.
func isBehind(reason string) bool { return strings.HasPrefix(reason, "behind") }
