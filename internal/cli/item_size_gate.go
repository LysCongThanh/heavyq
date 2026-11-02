package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/LysCongThanh/heavyq/internal/anchor"
	"github.com/LysCongThanh/heavyq/internal/history"
	"github.com/LysCongThanh/heavyq/internal/lease"
	"github.com/LysCongThanh/heavyq/internal/ledgerindex"
	"github.com/LysCongThanh/heavyq/internal/preflight"
	"github.com/LysCongThanh/heavyq/internal/repoconfig"
)

// Why the size gate refuses an item (the `code` of the `refused` JSON object).
const (
	refusedNoProof    = "no_proof"         // nothing can show the item is finished
	refusedProofBroke = "proof_cannot_run" // the proof names a missing gate or program
	refusedOversized  = "oversized"        // proof p50 x iterations is over the lease budget
)

// sizeRefusal is the size gate's "no" for one item.
type sizeRefusal struct {
	Item       string `json:"item"`
	Code       string `json:"code"`
	Reason     string `json:"reason"`
	Next       string `json:"next"`
	ProofP50S  int    `json:"proof_p50_s,omitempty"`
	Iterations int    `json:"iterations,omitempty"`
	ExpectedS  int    `json:"expected_s,omitempty"`
	BudgetS    int    `json:"budget_s,omitempty"`
	// Skipped lists the other items `--next` passed over for the same reason.
	Skipped []string `json:"skipped,omitempty"`
}

// judgeItemSize decides whether an item is small enough for one lease. The
// proof facts come from proofFacts (the preflight anchor check reads the same):
// no proof, a proof that cannot run, or a proof whose typical duration times the
// expected iterations is over the lease budget are refused. A proof that never
// passed here has no known duration: it is allowed, with a warning.
func judgeItemSize(it anchor.Item, label, problem string, est preflight.Estimate, lim repoconfig.Items, ttl time.Duration) (ref *sizeRefusal, warn string) {
	edit := editAnchorCommand(it)
	switch {
	case it.Proof.Kind == "":
		return &sizeRefusal{Item: it.ID, Code: refusedNoProof,
			Reason: fmt.Sprintf("item %s has no proof: nothing could show it is finished", it.ID),
			Next:   fmt.Sprintf("add `proof: <command>` to %s: %s", it.ID, edit)}, ""
	case problem != "":
		return &sizeRefusal{Item: it.ID, Code: refusedProofBroke,
			Reason: fmt.Sprintf("item %s: its proof cannot run: %s", it.ID, problem),
			Next:   fmt.Sprintf("fix the proof of %s: %s", it.ID, edit)}, ""
	case !est.Known:
		return nil, fmt.Sprintf("item %s: no known duration for its proof (%s), it has never passed here, so its size is a guess", it.ID, oneLine(label, 80))
	}
	budget := time.Duration(float64(ttl) * lim.MaxProofShare)
	expected := est.P50 * time.Duration(lim.Iterations)
	if expected <= budget {
		return nil, ""
	}
	return &sizeRefusal{Item: it.ID, Code: refusedOversized,
		Reason: fmt.Sprintf("item %s is too big for one %s lease: its proof takes about %s (p50), times %d iterations is %s, over the %s budget",
			it.ID, sizeDur(ttl), sizeDur(est.P50), lim.Iterations, sizeDur(expected), sizeDur(budget)),
		Next:      fmt.Sprintf("split %s into ≤3 items, each with a faster proof: %s (or `heavyq item claim %s --force`)", it.ID, edit, it.ID),
		ProofP50S: int(est.P50.Seconds()), Iterations: lim.Iterations,
		ExpectedS: int(expected.Seconds()), BudgetS: int(budget.Seconds())}, ""
}

// editAnchorCommand opens the file and line an item was written on.
func editAnchorCommand(it anchor.Item) string {
	return fmt.Sprintf("${EDITOR:-vi} +%d %s", it.Line, it.Source)
}

// sizeDur writes a duration for a sentence: whole minutes as "30m", under an hour
// to the second ("11m40s"), otherwise to the minute.
func sizeDur(d time.Duration) string {
	switch {
	case d%time.Minute == 0:
		return fmt.Sprintf("%dm", int(d/time.Minute))
	case d < time.Hour:
		return d.Round(time.Second).String()
	}
	return fmt.Sprintf("%dm", int(d.Round(time.Minute)/time.Minute))
}

// sizeGate applies judgeItemSize to the items of one claim. The ledger is read
// once, and only when an item with a proof is looked at.
type sizeGate struct {
	set    *itemSet
	stderr io.Writer
	ttl    time.Duration
	durs   preflight.Durations
	loaded bool
}

func (c *itemCtx) newSizeGate() *sizeGate {
	ttl := c.store.TTL
	if ttl <= 0 {
		ttl = lease.DefaultTTL
	}
	return &sizeGate{set: c.itemSet, stderr: c.stderr, ttl: ttl}
}

// check judges one item; warn is "" unless the duration is unknown.
func (g *sizeGate) check(it anchor.Item) (*sizeRefusal, string) {
	if it.Proof.Kind != "" && !g.loaded {
		g.loaded = true
		recs, _, err := ledgerindex.Recent(history.DefaultPath(), time.Now().Add(-ledgerWindow))
		if err != nil { // no durations: every proof reads as unknown, which only warns
			fmt.Fprintln(g.stderr, "heavyq: ledger unreadable, so proof durations are unknown:", err)
		}
		g.durs = preflight.P50ByCmdHash(recs)
	}
	label, problem, est, _ := proofFacts(g.set, it, g.durs)
	return judgeItemSize(it, label, problem, est, g.set.repo.ItemLimits(), g.ttl)
}

// refuseSize prints a size refusal: the reason, then the `next:` line (always:
// it is the point of the refusal), or the `refused` object with --json.
func (c *itemCtx) refuseSize(ref *sizeRefusal, asJSON bool) int {
	fmt.Fprintln(c.stderr, "heavyq:", ref.Reason)
	if len(ref.Skipped) > 0 {
		fmt.Fprintf(c.stderr, "heavyq: also skipped: %s\n", strings.Join(ref.Skipped, ", "))
	}
	fmt.Fprintln(c.stderr, "next: "+ref.Next)
	if asJSON {
		enc := json.NewEncoder(c.stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(struct {
			Refused *sizeRefusal `json:"refused"`
		}{ref}); err != nil {
			fmt.Fprintln(c.stderr, "heavyq:", err)
		}
	}
	return 1
}
