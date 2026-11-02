package preflight

import (
	"strings"
	"testing"
	"time"
)

func item(id string, mod func(*ItemFact)) ItemFact {
	it := ItemFact{ID: id, Where: ".heavyq/anchor.md:3", State: "open", Proof: "go test ./...",
		Est: Estimate{P50: 2 * time.Minute, Known: true}}
	if mod != nil {
		mod(&it)
	}
	return it
}

func TestCheckAnchor(t *testing.T) {
	const path = ".heavyq/anchor.md"
	base := func() AnchorInput {
		return AnchorInput{Goal: "ship", AnchorPath: path, Items: []ItemFact{item("a", nil), item("b", nil)},
			Parallel: 2, LeaseTTL: 30 * time.Minute}
	}
	cases := []struct {
		name   string
		mutate func(*AnchorInput)
		want   []string // "severity:substring" per expected finding, in order
	}{
		{"all good", func(*AnchorInput) {}, nil},
		{"no goal", func(in *AnchorInput) { in.Goal = "  " }, []string{"block:no goal"}},
		{"no items", func(in *AnchorInput) { in.Items = nil }, []string{"block:no items"}},
		{"everything proven", func(in *AnchorInput) {
			in.Items = []ItemFact{item("a", func(i *ItemFact) { i.State, i.Done = "proven", true })}
		}, []string{"block:already proven"}},
		{"proven but not ticked still counts as open work", func(in *AnchorInput) {
			in.Items = []ItemFact{item("a", func(i *ItemFact) { i.State = "proven" })}
		}, nil},
		{"item without proof", func(in *AnchorInput) {
			in.Items[1] = item("b", func(i *ItemFact) { i.Proof = "" })
		}, []string{"block:item b has no proof"}},
		{"proof cannot run", func(in *AnchorInput) {
			in.Items[0] = item("a", func(i *ItemFact) { i.ProofProblem = `no gate "web"` })
		}, []string{`block:no gate "web"`}},
		{"blocked items are the dependency check's business", func(in *AnchorInput) {
			in.Items[1] = item("b", func(i *ItemFact) { i.State, i.Proof = "blocked", "" })
		}, nil},
		{"unknown duration", func(in *AnchorInput) {
			in.Items[0] = item("a", func(i *ItemFact) { i.Est = Estimate{} })
		}, []string{"warn:no known duration for the proof of a"}},
		{"item over the lease budget", func(in *AnchorInput) {
			in.Items[0] = item("a", func(i *ItemFact) { i.Est = Estimate{P50: 45 * time.Minute, Known: true} })
		}, []string{"warn:proof longer than the 30m0s lease: a (45m0s)"}},
		{"proofs longer than --hours", func(in *AnchorInput) {
			in.Hours = 0.5
			in.Items = []ItemFact{
				item("a", func(i *ItemFact) { i.Est = Estimate{P50: 25 * time.Minute, Known: true} }),
				item("b", func(i *ItemFact) { i.Est = Estimate{P50: 25 * time.Minute, Known: true} }),
				item("c", func(i *ItemFact) { i.Est = Estimate{P50: 25 * time.Minute, Known: true} }),
				item("d", func(i *ItemFact) { i.Est = Estimate{P50: 25 * time.Minute, Known: true} }),
			}
		}, []string{"warn:the proofs alone take about 50m0s across 2 lanes, more than --hours 0.5"}},
		{"hours not stated skips the budget", func(in *AnchorInput) {
			in.Items[0].Est = Estimate{P50: 20 * time.Hour, Known: true}
			in.LeaseTTL = 0
		}, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			in := base()
			c.mutate(&in)
			assertFindings(t, CheckAnchor(in), c.want)
		})
	}
}

func TestAnchorFindingsNameTheFix(t *testing.T) {
	in := AnchorInput{AnchorPath: "a.md", Items: []ItemFact{item("x", func(i *ItemFact) { i.Proof = "" })}}
	fs := CheckAnchor(in)
	if len(fs) != 2 {
		t.Fatalf("want goal + proof findings: %+v", fs)
	}
	for _, f := range fs {
		if f.Fix == "" || !strings.HasPrefix(f.Fix, "edit a.md") && !strings.HasPrefix(f.Fix, "edit .heavyq/anchor.md") {
			t.Errorf("finding without an edit hint: %+v", f)
		}
	}
}

func TestUnknownDurationsAreAggregated(t *testing.T) {
	var items []ItemFact
	for _, id := range []string{"a", "b", "c", "d", "e", "f", "g"} {
		items = append(items, item(id, func(i *ItemFact) { i.Est = Estimate{} }))
	}
	fs := CheckAnchor(AnchorInput{Goal: "g", AnchorPath: "p", Items: items})
	if len(fs) != 1 || !strings.Contains(fs[0].Text, "a, b, c, d, e and 2 more") {
		t.Fatalf("one finding naming 5 and counting the rest: %+v", fs)
	}
}

// assertFindings checks "severity:substring" expectations in order.
func assertFindings(t *testing.T, got []Finding, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %d findings, want %d: %+v", len(got), len(want), got)
	}
	for i, w := range want {
		sev, sub, _ := strings.Cut(w, ":")
		if string(got[i].Severity) != sev || !strings.Contains(got[i].Text, sub) {
			t.Errorf("finding %d = %s %q, want %s containing %q", i, got[i].Severity, got[i].Text, sev, sub)
		}
		if got[i].Fix == "" {
			t.Errorf("finding %d has no fix: %+v", i, got[i])
		}
	}
}
