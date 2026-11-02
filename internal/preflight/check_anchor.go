package preflight

import (
	"fmt"
	"math"
	"strings"
	"time"
)

// Item states the checks care about (the words of internal/itemstatus).
const (
	stateProven  = "proven"
	stateBlocked = "blocked"
)

// ItemFact is one anchor item with what the checks need to know about it.
type ItemFact struct {
	ID    string
	Text  string
	Where string // "<file>:<line>", for "edit ..." fixes
	State string // internal/itemstatus state
	Done  bool   // box ticked, or marked done
	// Proof as written ("gate:web" or the command); "" when none is declared.
	Proof string
	// ProofProblem is why the declared proof cannot run at all (no such gate,
	// the program is not installed); "" when it can.
	ProofProblem string
	Needs        string   // what a human must provide; from the file or an item block
	NeedsInFile  bool     // Needs is a `needs:` span on the anchor line
	After        []string // ids this item waits on
	// Est is how long the proof takes, from the ledger.
	Est Estimate
}

// Estimate is the typical duration of an item's proof.
type Estimate struct {
	P50   time.Duration // sum of the median run time of each command
	Known bool          // false when any command has no passing run in the ledger
}

// Finished is true for an item that is done and still proven: nothing left to run.
func (i ItemFact) Finished() bool { return i.State == stateProven && i.Done }

// Blocked is true for an item a human must unblock first.
func (i ItemFact) Blocked() bool { return i.State == stateBlocked || i.Needs != "" }

// Open is true for an item the run would work on.
func (i ItemFact) Open() bool { return !i.Finished() && !i.Blocked() }

// AnchorInput is what CheckAnchor reads.
type AnchorInput struct {
	Goal       string
	AnchorPath string
	Items      []ItemFact
	Hours      float64       // 0 = no stated length
	Parallel   int           // lanes planned
	LeaseTTL   time.Duration // how long a lease lives without a renewal
}

// maxNamed is how many item ids one aggregated finding names.
const maxNamed = 5

// CheckAnchor: a goal and items exist, every open item has a runnable proof,
// its duration is known, and no item is bigger than the lease budget.
func CheckAnchor(in AnchorInput) []Finding {
	var out []Finding
	if strings.TrimSpace(in.Goal) == "" {
		out = append(out, Finding{Severity: Block, Subject: in.AnchorPath,
			Text: fmt.Sprintf("no goal in %s: nothing says what the run is for", in.AnchorPath),
			Fix:  fmt.Sprintf("edit %s: add a line `# Goal: <what done looks like>`", in.AnchorPath)})
	}
	if len(in.Items) == 0 {
		return append(out, Finding{Severity: Block, Subject: in.AnchorPath,
			Text: fmt.Sprintf("no items in %s: the agent has nothing to claim", in.AnchorPath),
			Fix:  fmt.Sprintf("edit %s: add lines like \"- [ ] api: what to build `proof: go test ./...`\"", in.AnchorPath)})
	}
	var open []ItemFact
	for _, it := range in.Items {
		if it.Open() {
			open = append(open, it)
		}
	}
	if len(open) == 0 && countBlocked(in.Items) == 0 {
		return append(out, Finding{Severity: Block, Subject: in.AnchorPath,
			Text: "every item is already proven: there is nothing left to run",
			Fix:  "heavyq item ls --all"})
	}
	var unknown, long, shelly []string
	var total time.Duration
	for _, it := range open {
		switch {
		case it.Proof == "":
			out = append(out, Finding{Severity: Block, Subject: it.ID,
				Text: fmt.Sprintf("item %s has no proof: it can only be ticked by hand, never proven", it.ID),
				Fix:  fmt.Sprintf("edit %s: add `proof: <command>` to %s", it.Where, it.ID)})
			continue
		case it.ProofProblem != "":
			out = append(out, Finding{Severity: Block, Subject: it.ID,
				Text: fmt.Sprintf("item %s: its proof cannot run: %s", it.ID, it.ProofProblem),
				Fix:  fmt.Sprintf("edit %s: fix the proof of %s", it.Where, it.ID)})
			continue
		}
		if proofNeedsShell(it.Proof) {
			shelly = append(shelly, it.ID)
		}
		if !it.Est.Known {
			unknown = append(unknown, it.ID)
			continue
		}
		total += it.Est.P50
		if in.LeaseTTL > 0 && it.Est.P50 > in.LeaseTTL {
			long = append(long, fmt.Sprintf("%s (%s)", it.ID, round(it.Est.P50)))
		}
	}
	if len(shelly) > 0 {
		out = append(out, Finding{Severity: Warn, Subject: named(shelly),
			Text: fmt.Sprintf("the proof of %s needs `sh -c` (a pipe, quotes or another shell feature): a harness that vets commands refuses `heavyq ... -- sh -c '...'`, so an agent cannot run it by hand; a gate holds the same text and runs with `heavyq gate run <name>`", named(shelly)),
			Fix:  fmt.Sprintf("edit heavyq.toml: add a [[gate]] with that command, then edit %s: `proof: gate:<name>`", shelly0Where(open, shelly[0]))})
	}
	if len(unknown) > 0 {
		out = append(out, Finding{Severity: Warn, Subject: named(unknown),
			Text: fmt.Sprintf("no known duration for the proof of %s: it has never passed here, so the run length is a guess", named(unknown)),
			Fix:  "heavyq item done " + unknown[0] + " --run"})
	}
	if len(long) > 0 {
		out = append(out, Finding{Severity: Warn, Subject: named(long),
			Text: fmt.Sprintf("proof longer than the %s lease: %s; only a heavyq run renews a lease, so split the item or keep other runs going", round(in.LeaseTTL), named(long)),
			Fix:  "edit " + in.AnchorPath + ": split the item into smaller ones"})
	}
	if in.Hours > 0 && in.Parallel > 0 && total > 0 {
		need := total / time.Duration(in.Parallel)
		if budget := time.Duration(in.Hours * float64(time.Hour)); need > budget {
			out = append(out, Finding{Severity: Warn,
				Text: fmt.Sprintf("the proofs alone take about %s across %d lanes, more than --hours %s", round(need), in.Parallel, trimFloat(in.Hours)),
				Fix:  fmt.Sprintf("heavyq preflight --hours %d", int(math.Ceil(need.Hours())))})
		}
	}
	return out
}

// proofNeedsShell is true for a command proof (not `gate:<name>`) that only a
// shell can run: it has a pipe, quotes, a redirect, a variable or a glob.
func proofNeedsShell(proof string) bool {
	return !strings.HasPrefix(proof, "gate:") && strings.ContainsAny(proof, "|&;<>()$`\"'\\*?\n")
}

// shelly0Where is the "<file>:<line>" of the item called id among items.
func shelly0Where(items []ItemFact, id string) string {
	for _, it := range items {
		if it.ID == id {
			return it.Where
		}
	}
	return id
}

func countBlocked(items []ItemFact) int {
	n := 0
	for _, it := range items {
		if it.Blocked() {
			n++
		}
	}
	return n
}

// named lists up to maxNamed names, then "and N more".
func named(names []string) string {
	if len(names) <= maxNamed {
		return strings.Join(names, ", ")
	}
	return fmt.Sprintf("%s and %d more", strings.Join(names[:maxNamed], ", "), len(names)-maxNamed)
}

// round shortens a duration for a sentence: whole seconds under a minute,
// whole minutes under 3 hours, else one decimal of hours.
func round(d time.Duration) time.Duration {
	switch {
	case d < time.Minute:
		return d.Round(time.Second)
	case d < 3*time.Hour:
		return d.Round(time.Minute)
	}
	return d.Round(6 * time.Minute)
}

func trimFloat(f float64) string {
	s := fmt.Sprintf("%.1f", f)
	return strings.TrimSuffix(s, ".0")
}
