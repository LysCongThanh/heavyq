package cli

import (
	"strings"
	"testing"
	"time"

	"github.com/LysCongThanh/heavyq/internal/anchor"
	"github.com/LysCongThanh/heavyq/internal/preflight"
	"github.com/LysCongThanh/heavyq/internal/repoconfig"
)

func TestJudgeItemSize(t *testing.T) {
	cmd := anchor.Proof{Kind: anchor.ProofCmd, Value: "go test ./..."}
	known := func(d time.Duration) preflight.Estimate { return preflight.Estimate{P50: d, Known: true} }
	lim := repoconfig.DefaultItems()
	ttl := 30 * time.Minute
	cases := []struct {
		name    string
		proof   anchor.Proof
		problem string
		est     preflight.Estimate
		lim     repoconfig.Items
		code    string // "" = allowed
		warn    bool
	}{
		{"no proof", anchor.Proof{}, "", preflight.Estimate{}, lim, refusedNoProof, false},
		{"proof cannot run", cmd, "`go` is not installed", known(time.Minute), lim, refusedProofBroke, false},
		{"fits", cmd, "", known(5 * time.Minute), lim, "", false},
		{"exactly the budget", cmd, "", known(10 * time.Minute), lim, "", false},
		{"one second over", cmd, "", known(10*time.Minute + time.Second), lim, refusedOversized, false},
		{"unknown duration", cmd, "", preflight.Estimate{}, lim, "", true},
		{"one iteration", cmd, "", known(25 * time.Minute), repoconfig.Items{MaxProofShare: 1, Iterations: 1}, "", false},
		{"half the lease", cmd, "", known(6 * time.Minute), repoconfig.Items{MaxProofShare: 0.5, Iterations: 3}, refusedOversized, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			it := anchor.Item{ID: "x", Proof: c.proof, Source: ".heavyq/anchor.md", Line: 4}
			ref, warn := judgeItemSize(it, proofLabel(c.proof), c.problem, c.est, c.lim, ttl)
			switch {
			case c.code == "" && ref != nil:
				t.Fatalf("refused: %+v", ref)
			case c.code != "" && (ref == nil || ref.Code != c.code):
				t.Fatalf("refusal = %+v, want code %s", ref, c.code)
			case (warn != "") != c.warn:
				t.Fatalf("warning = %q, want warning: %v", warn, c.warn)
			}
			if ref != nil && !strings.Contains(ref.Next, "+4 .heavyq/anchor.md") {
				t.Errorf("next %q does not say where to edit", ref.Next)
			}
		})
	}
}
