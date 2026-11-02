package receipts

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/LysCongThanh/heavyq/internal/history"
)

// A ledger whose every name is a canary: none of them may reach a share card,
// its JSON or the exact summary.
var canaries = []string{
	"lane-canary-billing", "repo-canary-hash", "/home/canary/secret-project", "cmd-canary --token=abc",
	"machine-canary-box", "session-canary", "item-canary", "gate-canary", "branch-canary",
}

func canaryLedger() []history.Record {
	var recs []history.Record
	for i := 0; i < 60; i++ {
		r := history.Record{
			End: t1.Add(-time.Duration(i+1) * time.Hour), Machine: "local", Lane: canaries[0] + string(rune('a'+i%5)),
			WaitSecs: float64(i % 7 * 20), RunSecs: 30, Exit: i % 9 / 8, RunID: "run" + string(rune('a'+i%4)),
			Cmd: canaries[3], Repo: canaries[1], Worktree: canaries[2], Subdir: canaries[2], Session: canaries[5],
			Item: canaries[6], Gate: canaries[7], Commit: canaries[8], Tree: canaries[8],
		}
		switch i % 10 {
		case 3:
			r.Machine = canaries[4]
		case 5:
			r.Cache = "hit"
		case 7:
			r.Pools = "port=4100,4101 " + canaries[0] + "=v"
		}
		recs = append(recs, r)
	}
	return recs
}

func TestShareHasNoNamesAnywhere(t *testing.T) {
	r := Compute(canaryLedger(), t0, t1)
	if r.Runs == 0 || r.RemoteRuns == 0 || r.DuplicatesAvoided == 0 || r.Lanes == 0 {
		t.Fatalf("the canary ledger must exercise every line: %+v", r)
	}
	s := NewShare(r, "1.2.3", "week")
	j, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	exact, _ := json.Marshal(r)
	for what, text := range map[string]string{"card": s.Text, "share json": string(j), "exact json": string(exact), "exact text": r.Text("week")} {
		for _, c := range canaries {
			if strings.Contains(text, c) {
				t.Errorf("%s leaks %q:\n%s", what, c, text)
			}
		}
	}
}

func TestShareCardShape(t *testing.T) {
	r := Compute(canaryLedger(), t0, t1)
	s := NewShare(r, "1.2.3", "week")
	lines := strings.Split(strings.TrimRight(s.Text, "\n"), "\n")
	if len(lines) > 12 {
		t.Errorf("card has %d lines, budget 12:\n%s", len(lines), s.Text)
	}
	width := utf8.RuneCountInString(lines[0])
	for _, ln := range lines {
		if utf8.RuneCountInString(ln) != width {
			t.Errorf("ragged card (%d != %d): %q", utf8.RuneCountInString(ln), width, ln)
		}
	}
	for _, want := range []string{"heavyq receipts · last 7 days", "heavyq 1.2.3", "runs/day", "duplicate runs avoided", "waited their turn", "wait p50", "served"} {
		if !strings.Contains(s.Text, want) {
			t.Errorf("card lacks %q:\n%s", want, s.Text)
		}
	}
	if s.Version != "1.2.3" || s.Window != "last 7 days" {
		t.Errorf("share meta: %+v", s)
	}
}

// The card is for posting: it must not give the exact count away.
func TestShareRoundsInsteadOfCounting(t *testing.T) {
	recs := make([]history.Record, 43)
	for i := range recs {
		recs[i] = rec(nil)
	}
	r := Compute(recs, t0, t1) // 43 runs in 7 days: 6.1/day
	s := NewShare(r, "dev", "week")
	if strings.Contains(s.Text, "43") || !strings.Contains(s.Text, "~6 runs/day") {
		t.Errorf("want ~6 runs/day and no 43:\n%s", s.Text)
	}
}

func TestShareOfAnEmptyWindow(t *testing.T) {
	s := NewShare(Compute(nil, t0, t1), "dev", "day")
	if !strings.Contains(s.Text, "no heavy runs in this window") || !strings.Contains(s.Text, "last 24 hours") {
		t.Errorf("empty card:\n%s", s.Text)
	}
}

func TestShareLeavesOutMetricsThatAreZero(t *testing.T) {
	s := NewShare(Compute([]history.Record{rec(nil), rec(nil)}, t0, t1), "dev", "month")
	for _, absent := range []string{"duplicate", "flagged", "offloaded", "pool", "waited their turn"} {
		if strings.Contains(s.Text, absent) {
			t.Errorf("card mentions %q although it is zero:\n%s", absent, s.Text)
		}
	}
	if !strings.Contains(s.Text, "2 runs in total") {
		t.Errorf("a window under one run a day is counted in total:\n%s", s.Text)
	}
}
