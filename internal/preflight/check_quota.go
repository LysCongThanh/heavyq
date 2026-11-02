package preflight

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

// Quota is what the status line recorded about the subscription's rate limits:
// percent used of the 5 hour and the 7 day window. A nil field is unknown.
type Quota struct {
	FiveHourPct *float64
	SevenDayPct *float64
	At          time.Time // when the meta file was written
}

// Thresholds of the quota heuristics (percent used). They are deliberately
// crude: the point is a warning before a night, not a forecast.
const (
	fiveHourWarnPct = 80 // the window is nearly spent and a run starts now
	longRunFiveHour = 50 // a run longer than one window, started half spent
	sevenDayWarnPct = 85 // the week is nearly spent
	longRunSevenDay = 70 // a run of a day or more, week 70% spent
	quotaStaleAfter = time.Hour
	maxMetaBytes    = 64 << 10
)

// ReadQuota reads the newest <common>/heavyq/sessions/*.meta.json that carries
// rate_limits. The file is optional (the status line writes it): no file, an
// unreadable or stale one, all mean "unknown", reported as nil.
func ReadQuota(common string, now time.Time) *Quota {
	files, _ := filepath.Glob(filepath.Join(common, "heavyq", "sessions", "*.meta.json"))
	var best *Quota
	for _, f := range files {
		info, err := os.Stat(f)
		if err != nil || !info.Mode().IsRegular() || (best != nil && !info.ModTime().After(best.At)) {
			continue
		}
		if q := readMeta(f); q != nil {
			q.At = info.ModTime()
			best = q
		}
	}
	if best == nil || now.Sub(best.At) > quotaStaleAfter {
		return nil
	}
	return best
}

func readMeta(path string) *Quota {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	var doc struct {
		RateLimits struct {
			FiveHour *float64 `json:"five_hour_pct"`
			SevenDay *float64 `json:"seven_day_pct"`
		} `json:"rate_limits"`
	}
	if json.NewDecoder(io.LimitReader(f, maxMetaBytes)).Decode(&doc) != nil {
		return nil
	}
	if doc.RateLimits.FiveHour == nil && doc.RateLimits.SevenDay == nil {
		return nil
	}
	return &Quota{FiveHourPct: doc.RateLimits.FiveHour, SevenDayPct: doc.RateLimits.SevenDay}
}

// CheckQuota warns when the recorded headroom looks short for a run of hours
// (0 = no stated length). It never blocks: the numbers are a hint.
func CheckQuota(q *Quota, hours float64) []Finding {
	if q == nil {
		return nil
	}
	var out []Finding
	short := func(text string) {
		out = append(out, Finding{Severity: Warn, Text: text, Fix: "heavyq preflight --hours 2"})
	}
	if p := q.FiveHourPct; p != nil {
		switch {
		case *p >= fiveHourWarnPct:
			short(fmt.Sprintf("the 5 hour window is %.0f%% used: the agent will likely hit the limit and stop", *p))
		case hours > 5 && *p >= longRunFiveHour:
			short(fmt.Sprintf("the 5 hour window is %.0f%% used and the run lasts %s h: it will cross a window boundary early", *p, trimFloat(hours)))
		}
	}
	if p := q.SevenDayPct; p != nil {
		switch {
		case *p >= sevenDayWarnPct:
			short(fmt.Sprintf("the 7 day quota is %.0f%% used: a long run may exhaust the week", *p))
		case hours >= 12 && *p >= longRunSevenDay:
			short(fmt.Sprintf("the 7 day quota is %.0f%% used and the run lasts %s h", *p, trimFloat(hours)))
		}
	}
	return out
}
