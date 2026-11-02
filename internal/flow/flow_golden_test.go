package flow

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LysCongThanh/heavyq/internal/history"
	"github.com/LysCongThanh/heavyq/internal/lease"
)

var update = flag.Bool("update", false, "rewrite the golden files of internal/flow/testdata")

// fixture is a recorded situation: what the ledger, the lease events log,
// itemstatus and the hooks said at Now. It is data, so a golden file can only
// change when the derivation does.
type fixture struct {
	Now     time.Time        `json:"now"`
	Records []history.Record `json:"records"`
	Leases  []lease.Event    `json:"leases"`
	Items   []ItemFact       `json:"items"`
	Hooks   []Event          `json:"hooks"`
}

func loadFixture(t *testing.T, name string) Input {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var f fixture
	if err := json.Unmarshal(b, &f); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return Input{Now: f.Now, Repo: "/work/app", Records: f.Records, Leases: f.Leases, Items: f.Items, Hooks: f.Hooks}
}

// render is the golden form: one block per timeline, spans as clock times, then
// the snapshot facts a reader would act on.
func render(in Input) string {
	var b strings.Builder
	clock := func(t time.Time) string { return t.UTC().Format("15:04:05") }
	for _, t := range Timelines(in) {
		fmt.Fprintf(&b, "%s %s lane=%s session=%s", t.Kind, t.Key, t.Lane, t.Session)
		if t.Final != "" {
			fmt.Fprintf(&b, " final=%s@%s", t.Final, clock(t.FinalAt))
		}
		b.WriteString("\n")
		for _, s := range t.Spans {
			fmt.Fprintf(&b, "  %s-%s %-13s %s\n", clock(s.Start), clock(s.End), s.State, s.Duration())
		}
	}
	snap := Build(in)
	for _, n := range snap.NeedsYou {
		fmt.Fprintf(&b, "needs_you item=%q lane=%s since=%s reason=%q\n", n.Item, n.Lane, clock(n.Since), n.Reason)
	}
	for _, l := range snap.Lanes {
		fmt.Fprintf(&b, "lane %s state=%s item=%q since=%s\n", l.Lane, l.State, l.Item, clock(l.Since))
	}
	c := snap.Constraint
	fmt.Fprintf(&b, "constraint %s share=%.2f significant=%v\n", c.State, c.Share, c.Significant)
	fmt.Fprintf(&b, "items %+v\n", snap.Items)
	return b.String()
}

func TestGoldenTimelines(t *testing.T) {
	for _, name := range []string{"item_rework_proven", "blocked_and_unproven", "session_only"} {
		t.Run(name, func(t *testing.T) {
			got := render(loadFixture(t, name))
			path := filepath.Join("testdata", name+".golden.txt")
			if *update {
				if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("%v (run go test ./internal/flow -update)", err)
			}
			if string(want) != got {
				t.Errorf("golden mismatch for %s\n--- got\n%s--- want\n%s", name, got, want)
			}
		})
	}
}
