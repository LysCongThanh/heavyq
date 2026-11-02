package collect

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/LysCongThanh/heavyq/internal/flow"
)

var t0 = time.Date(2026, 11, 2, 10, 0, 0, 0, time.UTC)

func at(min int) time.Time { return t0.Add(time.Duration(min) * time.Minute) }

func iso(t time.Time) string { return t.UTC().Format(time.RFC3339) }

const zeroTime = "0001-01-01T00:00:00Z"

// fakeGH answers the two calls of the collector and counts them.
type fakeGH struct {
	prs, runs string
	err       error
	calls     int
}

func (f *fakeGH) run(_ context.Context, _ string, args ...string) ([]byte, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	if args[0] == "pr" {
		return []byte(f.prs), nil
	}
	return []byte(f.runs), nil
}

func runJSON(branch, sha, status, conclusion string, created, started, updated time.Time) string {
	return `{"headBranch":"` + branch + `","headSha":"` + sha + `","event":"pull_request","status":"` + status +
		`","conclusion":"` + conclusion + `","createdAt":"` + iso(created) + `","startedAt":"` + iso(started) + `","updatedAt":"` + iso(updated) + `"}`
}

func prJSON(branch, state string, draft bool, merged time.Time) string {
	m := zeroTime
	if !merged.IsZero() {
		m = iso(merged)
	}
	d := "false"
	if draft {
		d = "true"
	}
	return `{"number":1,"headRefName":"` + branch + `","state":"` + state + `","isDraft":` + d +
		`,"createdAt":"` + iso(at(0)) + `","mergedAt":"` + m + `","closedAt":"` + zeroTime + `"}`
}

func spansOf(ext []flow.External, item string, st flow.State) []flow.Span {
	var out []flow.Span
	for _, e := range ext {
		if e.Item == item && e.Span.State == st {
			out = append(out, e.Span)
		}
	}
	return out
}

func TestGHReportsCIWaitPerRunAndMergeWaitFromGreenToMerge(t *testing.T) {
	g := &fakeGH{
		prs: "[" + prJSON("feat/login", "MERGED", false, at(60)) + "," + prJSON("feat/other", "OPEN", false, time.Time{}) + "]",
		runs: "[" + strings.Join([]string{
			runJSON("feat/login", "a1", "completed", "failure", at(0), at(1), at(10)),
			runJSON("feat/login", "b2", "completed", "success", at(15), at(16), at(25)),
			runJSON("feat/login", "b2", "completed", "skipped", at(15), at(16), at(18)),
			runJSON("main", "m1", "completed", "success", at(0), at(0), at(30)), // not an item's branch
		}, ",") + "]",
	}
	o := Options{Items: []string{"login"}, GH: g.run, Now: func() time.Time { return at(30) }}
	ext, err := GH(o)("/repo", at(-60), at(30))
	if err != nil {
		t.Fatal(err)
	}
	ci := spansOf(ext, "login", flow.CIWait)
	if len(ci) != 3 || ci[0].Start != at(1) || ci[0].End != at(10) || ci[1].Duration() != 9*time.Minute {
		t.Errorf("one ci_wait per run, from its start to its end: %+v", ci)
	}
	mw := spansOf(ext, "login", flow.MergeWait)
	if len(mw) != 1 || mw[0].Start != at(25) || mw[0].End != at(30) {
		t.Errorf("green at 25, merged at 60 but the answer is only trusted to the horizon (30): %+v", mw)
	}
	if len(spansOf(ext, "other", flow.MergeWait)) != 0 {
		t.Error("a branch no item is named after has no spans")
	}
}

func TestGHOpenRunsEndAtTheHorizonAndFailedOrDraftWaitForNothing(t *testing.T) {
	g := &fakeGH{
		prs: "[" + prJSON("feat/login", "OPEN", false, time.Time{}) + "," + prJSON("feat/draft", "OPEN", true, time.Time{}) + "," +
			prJSON("feat/failing", "OPEN", false, time.Time{}) + "," + prJSON("feat/closed", "CLOSED", false, time.Time{}) + "]",
		runs: "[" + strings.Join([]string{
			runJSON("feat/login", "a", "in_progress", "", at(0), at(1), at(5)),
			runJSON("feat/draft", "d", "completed", "success", at(0), at(1), at(5)),
			runJSON("feat/failing", "f", "completed", "failure", at(0), at(1), at(5)),
			runJSON("feat/closed", "c", "completed", "success", at(0), at(1), at(5)),
		}, ",") + "]",
	}
	o := Options{Items: []string{"login", "draft", "failing", "closed"}, GH: g.run, Now: func() time.Time { return at(20) }}
	ext, _ := GH(o)("/repo", at(-60), at(20))
	if ci := spansOf(ext, "login", flow.CIWait); len(ci) != 1 || ci[0].End != at(20) {
		t.Errorf("a run still going is waited on until now: %+v", ci)
	}
	for _, id := range []string{"login", "draft", "failing", "closed"} {
		if mw := spansOf(ext, id, flow.MergeWait); len(mw) != 0 {
			t.Errorf("%s: no merge wait (running, draft, failed or closed): %+v", id, mw)
		}
	}
}

func TestGHOpenPRThatIsGreenWaitsForMergeUntilNow(t *testing.T) {
	g := &fakeGH{
		prs:  "[" + prJSON("feat/login", "OPEN", false, time.Time{}) + "]",
		runs: "[" + runJSON("feat/login", "a", "completed", "success", at(0), at(1), at(9)) + "]",
	}
	o := Options{Items: []string{"login"}, GH: g.run, Now: func() time.Time { return at(30) }}
	ext, _ := GH(o)("/repo", at(-60), at(30))
	if mw := spansOf(ext, "login", flow.MergeWait); len(mw) != 1 || mw[0].Start != at(9) || mw[0].End != at(30) {
		t.Errorf("%+v", mw)
	}
}

func TestGHCallsAtMostOncePerIntervalAndServesTheCache(t *testing.T) {
	common := t.TempDir()
	now := at(0)
	g := &fakeGH{
		prs:  "[" + prJSON("feat/login", "OPEN", false, time.Time{}) + "]",
		runs: "[" + runJSON("feat/login", "a", "in_progress", "", at(0), at(0), at(0)) + "]",
	}
	o := Options{Items: []string{"login"}, Common: common, GH: g.run, Now: func() time.Time { return now }}
	for i := 0; i < 4; i++ {
		now = at(i) // a refresh every minute
		if _, err := GH(o)("/repo", at(-60), now); err != nil {
			t.Fatal(err)
		}
	}
	if g.calls != 2 { // pr list and run list, once
		t.Fatalf("four refreshes inside the interval made %d gh calls, want 2", g.calls)
	}
	now = at(6)
	ext, err := GH(o)("/repo", at(-60), now)
	if err != nil || g.calls != 4 {
		t.Fatalf("after the interval gh is asked again: %d calls, %v", g.calls, err)
	}
	if ci := spansOf(ext, "login", flow.CIWait); len(ci) != 1 || ci[0].End != at(6) {
		t.Errorf("%+v", ci)
	}
}

func TestGHFailsOpenKeepsTheLastGoodAnswerAndBacksOff(t *testing.T) {
	common := t.TempDir()
	now := at(0)
	g := &fakeGH{
		prs:  "[" + prJSON("feat/login", "OPEN", false, time.Time{}) + "]",
		runs: "[" + runJSON("feat/login", "a", "in_progress", "", at(0), at(0), at(0)) + "]",
	}
	o := Options{Items: []string{"login"}, Common: common, GH: g.run, Now: func() time.Time { return now }}
	if _, err := GH(o)("/repo", at(-60), now); err != nil {
		t.Fatal(err)
	}
	g.err = errors.New("gh: not logged in")
	now = at(7) // the cache is old: gh is asked, and fails
	ext, err := GH(o)("/repo", at(-60), now)
	if err == nil || !strings.Contains(err.Error(), "not logged in") {
		t.Fatalf("the failure is reported for the log: %v", err)
	}
	ci := spansOf(ext, "login", flow.CIWait)
	if len(ci) != 1 || ci[0].End != at(5) {
		t.Errorf("the last good answer is kept, but its open run is cut at its horizon (fetched 0 + 5 min): %+v", ci)
	}
	calls := g.calls
	now = at(9)
	if _, err := GH(o)("/repo", at(-60), now); err != nil || g.calls != calls {
		t.Errorf("a failed call is remembered: no new call inside the interval (%d -> %d), err %v", calls, g.calls, err)
	}
}

func TestGHWithNothingToShowYieldsNothingNotAnError(t *testing.T) {
	g := &fakeGH{err: errors.New("exit status 4")}
	o := Options{Items: []string{"login"}, GH: g.run, Now: func() time.Time { return at(0) }}
	if ext, err := GH(o)("/repo", at(-60), at(0)); len(ext) != 0 || err == nil {
		t.Errorf("never fetched anything: no spans, the error is for the log: %v %v", ext, err)
	}
	if ext, err := GH(Options{GH: g.run})("/repo", at(-60), at(0)); ext != nil || err != nil || g.calls != 1 {
		t.Errorf("no items, nothing to attribute: gh is not even asked (%d calls)", g.calls)
	}
	t.Setenv("PATH", t.TempDir()) // no gh binary at all
	if ext, err := GH(Options{Items: []string{"login"}})("/repo", at(-60), at(0)); ext != nil || err != nil {
		t.Errorf("gh not installed is not an error: %v %v", ext, err)
	}
}

func TestGHDamagedOrFutureCacheIsAskedAgain(t *testing.T) {
	common := t.TempDir()
	if err := os.MkdirAll(common+"/heavyq/flow", 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ghCachePath(common), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	g := &fakeGH{prs: "[]", runs: "[]"}
	o := Options{Items: []string{"login"}, Common: common, GH: g.run, Now: func() time.Time { return at(0) }}
	_, _ = GH(o)("/repo", at(-60), at(0))
	if g.calls != 2 {
		t.Fatalf("a damaged cache is no cache: %d calls", g.calls)
	}
	future := &ghSnapshot{AttemptedAt: at(500), FetchedAt: at(500)}
	writeGHCache(common, future)
	_, _ = GH(o)("/repo", at(-60), at(0))
	if g.calls != 4 {
		t.Errorf("a cache stamped in the future (clock step) is stale: %d calls", g.calls)
	}
}
