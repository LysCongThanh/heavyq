package recap

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LysCongThanh/heavyq/internal/flowstate"
	"github.com/LysCongThanh/heavyq/internal/history"
	"github.com/LysCongThanh/heavyq/internal/itemstatus"
)

var update = flag.Bool("update", false, "rewrite the golden recaps")

func init() { time.Local = time.UTC }

var now = time.Date(2026, 11, 2, 14, 0, 0, 0, time.UTC)

func at(h, m int) time.Time { return time.Date(2026, 11, 2, h, m, 0, 0, time.UTC) }

const stateJSON = `{"schema":1,"repo":"/r","updated":"2026-11-02T13:59:55Z","goal":"ship login","run":null,
"items":{"total":8,"proven":5,"running":2,"blocked":1,"claimed_unproven":1,"open":1},
"needs_you":[{"item":"doc-7","lane":"docs","session":"s1","reason":"blocked: needs a Stripe test key","since":"2026-11-02T13:30:00Z"},
 {"item":"","lane":"web","session":"s2","reason":"permission_prompt: Bash(npm run e2e)","since":"2026-11-02T13:50:00Z"}],
"constraint":{"state":"ci_wait","share":0.41,"significant":true},
"throughput":{"proven_per_hour":2.1,"p50_claim_to_proof_s":1800,"window_s":14400,"n":9},
"lanes":[{"lane":"web","state":"proving","item":"web-1","since":"2026-11-02T13:20:00Z"},{"lane":"docs","state":"blocked","item":"doc-7","since":"2026-11-02T13:30:00Z"}],
"session":null}`

func fixtureFacts(t *testing.T) Facts {
	t.Helper()
	p := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(p, []byte(stateJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	st, err := flowstate.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	items := []Item{
		{ID: "api-1", State: itemstatus.Proven, Lane: "api", Commit: "3f2a9c1", At: at(12, 40), Done: true, Text: "Login endpoint", Session: "s1"},
		{ID: "api-2", State: itemstatus.Proven, Lane: "api", Commit: "8b1d0e2", At: at(13, 10), Text: "Token refresh", Session: "s1"},
		{ID: "api-3", State: itemstatus.Proven, Lane: "api", Commit: "c0ffee1", At: at(13, 40), Done: true, Text: "Rate limiting", Session: "s1"},
		{ID: "old-1", State: itemstatus.Proven, Commit: "aaaaaaa", At: at(9, 0), Done: true, Text: "Before the window"},
		{ID: "ui-1", State: itemstatus.Proven, Lane: "ui", Commit: "d00d123", At: at(13, 55), Text: "Form\x1b[31m markup", Session: "s3"},
		{ID: "doc-3", State: itemstatus.ClaimedDoneUnproven, Lane: "docs", Reason: "no proof declared", Text: "Write the guide"},
		{ID: "doc-7", State: itemstatus.Blocked, Lane: "docs", Needs: "a Stripe test key", Text: "Billing docs"},
		{ID: "web-1", State: itemstatus.Claimed, Lane: "web", Expires: at(14, 30), Text: "E2E suite"},
		{ID: "web-2", State: itemstatus.Open, Text: "Smoke"},
	}
	fail := func(end time.Time, runID, lane string, exit int, run float64) history.Record {
		return history.Record{End: end, Lane: lane, Exit: exit, RunSecs: run, Cmd: "go test ./internal/cli", RunID: runID, CmdHash: "h-" + runID}
	}
	loaded := fail(at(13, 20), "run-a", "api", 1, 100)
	loaded.Cores, loaded.Load1, loaded.MaxLoad = 8, 12, 9 // started over the load limit: the machine's fault
	cached := fail(at(13, 21), "run-a", "api", 1, 0)
	cached.Cache = "hit"
	return Facts{Now: now, Since: at(12, 0), Goal: "ship login", Items: items, State: st,
		Merged: func(c string) bool { return c == "3f2a9c1" || c == "c0ffee1" }, MergeRef: "main",
		Records: []history.Record{
			fail(at(12, 10), "run-a", "api", 1, 120), fail(at(12, 50), "run-a", "api", 1, 130), fail(at(13, 30), "run-a", "api", 1, 140),
			loaded, cached,
			fail(at(13, 0), "run-b", "web", 1, 60),  // once only: information, not waste
			fail(at(13, 5), "run-c", "docs", 0, 10), // passed
			fail(at(11, 0), "run-d", "web", 1, 50),  // before the window
			fail(at(11, 5), "run-d", "web", 1, 50),
		}}
}

func golden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run with -update)", err)
	}
	if got != string(want) {
		t.Errorf("%s differs:\n--- got\n%s--- want\n%s", name, got, want)
	}
}

// After an intended change: go test ./internal/recap -update, then read the diff.
func TestGoldenRecap(t *testing.T) {
	r := Build(fixtureFacts(t))
	golden(t, "recap.txt", Render(r, RenderOptions{}))
	golden(t, "recap.ascii.txt", Render(r, RenderOptions{ASCII: true}))
	golden(t, "recap.verbose.txt", Render(r, RenderOptions{Verbose: true}))
	j, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "recap.json", string(j)+"\n")
}

func TestRecapContent(t *testing.T) {
	r := Build(fixtureFacts(t))
	if r.Progress.Proven != 5 || r.Progress.Total != 9 {
		t.Errorf("progress %+v", r.Progress)
	}
	if len(r.Merged) != 2 || len(r.ProvenNotMerged) != 2 {
		t.Errorf("merged %d, proven %d: the item proven before the window is not 'what landed'", len(r.Merged), len(r.ProvenNotMerged))
	}
	if len(r.Wasted) != 1 || r.Wasted[0].Count != 3 || r.Wasted[0].Action != "heavyq why api" {
		t.Errorf("wasted: %+v (loaded, cached, single and out-of-window failures must not count)", r.Wasted)
	}
	if r.Best == nil || r.Best.Command != "heavyq item claim doc-7" {
		t.Errorf("best action: %+v, a blocked item comes first", r.Best)
	}
	if r.NeedsYou != 2 {
		t.Errorf("needs you = %d: the blocked item and the permission prompt, no double count", r.NeedsYou)
	}
	for name, lines := range map[string][]Line{"merged": r.Merged, "proven": r.ProvenNotMerged, "claimed": r.ClaimedUnproven,
		"blocked": r.Blocked, "wasted": r.Wasted, "in flight": r.InFlight} {
		for _, l := range lines {
			if l.Action == "" {
				t.Errorf("%s: %q has no command", name, l.Text)
			}
		}
	}
	if out := Render(r, RenderOptions{}); strings.Contains(out, "\x1b") {
		t.Error("untrusted item text must not reach the terminal as escape sequences")
	}
}

func TestSessionFilterAndUnknownMerge(t *testing.T) {
	f := fixtureFacts(t)
	f.Session, f.Merged = "s1", nil
	r := Build(f)
	if r.Progress.Total != 3 || len(r.Merged) != 0 || len(r.ProvenNotMerged) != 3 || r.MergeRef != "" {
		t.Errorf("session s1 with no merge information: %+v merged %d proven %d", r.Progress, len(r.Merged), len(r.ProvenNotMerged))
	}
	if len(r.Wasted) != 0 {
		t.Errorf("the failures carry no session: %+v", r.Wasted)
	}
	if out := Render(r, RenderOptions{}); !strings.Contains(out, "\nproven (3)\n") || strings.Contains(out, "merged into") {
		t.Errorf("without merge information the bucket is just 'proven':\n%s", out)
	}
}

func TestBestActionOrder(t *testing.T) {
	base := func() Facts {
		return Facts{Now: now, Since: at(12, 0), Items: []Item{{ID: "a", State: itemstatus.Open}}}
	}
	if r := Build(base()); r.Best == nil || r.Best.Command != "heavyq item claim --next" {
		t.Errorf("an idle repository starts the next item: %+v", r.Best)
	}
	f := base()
	f.Items = []Item{{ID: "a", State: itemstatus.Claimed, Lane: "x"}, {ID: "b", State: itemstatus.Open}}
	if r := Build(f); r.Best != nil {
		t.Errorf("work in flight and nothing wrong: no action, got %+v", r.Best)
	}
	f.Items = []Item{{ID: "a", State: itemstatus.ClaimedDoneUnproven}}
	if r := Build(f); r.Best == nil || r.Best.Command != "heavyq item done a --run" {
		t.Errorf("%+v", r.Best)
	}
	f.Items = nil
	if out := Render(Build(f), RenderOptions{}); !strings.Contains(out, "nothing to recap") {
		t.Errorf("empty recap: %q", out)
	}
}

func TestHeaderForecastWithoutState(t *testing.T) {
	f := Facts{Now: now, Since: at(12, 0), Items: []Item{{ID: "a", State: itemstatus.Open}, {ID: "b", State: itemstatus.Proven, At: at(13, 0), Commit: "abc"}}}
	r := Build(f)
	if r.Forecast.Status != "unknown" || r.Progress.Total != 2 {
		t.Errorf("no flow state: %+v", r.Forecast)
	}
	if out := Render(r, RenderOptions{}); !strings.Contains(out, "forecast unknown") {
		t.Errorf("%s", out)
	}
}
