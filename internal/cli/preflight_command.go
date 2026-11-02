package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/LysCongThanh/heavyq/internal/config"
	"github.com/LysCongThanh/heavyq/internal/gitutil"
	"github.com/LysCongThanh/heavyq/internal/history"
	"github.com/LysCongThanh/heavyq/internal/lease"
	"github.com/LysCongThanh/heavyq/internal/ledgerindex"
	"github.com/LysCongThanh/heavyq/internal/preflight"
	"github.com/LysCongThanh/heavyq/internal/runspan"
)

const preflightUsage = `usage: heavyq [--config FILE] preflight [--hours N] [--parallel N] [--start] [--json]

Before a long unattended run: GO or NO-GO, and a numbered list of what a human must answer or fix now, each
line ending with the command that fixes it. Checks the anchor (goal, items, runnable proofs with known
duration), dependencies (after:, open needs:), the environment (disk, remotes, slots, pools), Claude Code
permissions (commands that may prompt) and quota headroom. On GO it opens a run span; --start opens one even
on NO-GO. See docs/preflight.md.
Exit: 0 GO, 1 NO-GO, 2 usage.
`

// defaultParallel is how many lanes a run plans for when nothing says otherwise.
const defaultParallel = 3

// ledgerWindow is how far back proof durations are looked up.
const ledgerWindow = 30 * 24 * time.Hour

// preflightJSON is the `preflight --json` document: the report and the run span
// that was opened (null when none was).
type preflightJSON struct {
	preflight.Report
	Span *runspan.Span `json:"span"`
}

// preflightCommand implements `heavyq preflight`.
func preflightCommand(cfg *config.Config, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("preflight", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { fmt.Fprint(stderr, preflightUsage) }
	hours := fs.Float64("hours", 0, "planned length of the run in hours (0 = not stated)")
	parallel := fs.Int("parallel", 0, "lanes planned (default: lanes holding leases, else 3)")
	start := fs.Bool("start", false, "open the run span even on NO-GO")
	asJSON := fs.Bool("json", false, "machine-readable output (schema in docs/preflight.md)")
	if err := fs.Parse(args); err != nil {
		return ExitUsage
	}
	if fs.NArg() != 0 || *hours < 0 || *hours > 24*14 || *parallel < 0 || *parallel > 64 {
		fmt.Fprint(stderr, preflightUsage)
		return ExitUsage
	}
	wd, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(stderr, "heavyq:", err)
		return ExitUsage
	}
	set, err := loadItemSet(cfg, wd)
	if err != nil {
		fmt.Fprintln(stderr, "heavyq:", err)
		return ExitUsage
	}
	common, err := gitutil.CommonDir(set.root)
	if err != nil {
		fmt.Fprintln(stderr, "heavyq:", err)
		return ExitUsage
	}
	now := time.Now()
	if *parallel == 0 {
		*parallel = leasedLanes(set.store, now)
	}
	report, err := runPreflight(cfg, set, common, *hours, *parallel, now, stderr)
	if err != nil {
		fmt.Fprintln(stderr, "heavyq:", err)
		return 1
	}
	var span *runspan.Span
	if report.Verdict == preflight.Go || *start {
		s, err := openRunSpan(common, report, now)
		if err != nil {
			fmt.Fprintln(stderr, "heavyq: run span not opened:", err)
			return 1
		}
		span = &s
	}
	if *asJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		enc.SetEscapeHTML(false)
		if err := enc.Encode(preflightJSON{Report: report, Span: span}); err != nil {
			fmt.Fprintln(stderr, "heavyq:", err)
			return 1
		}
	} else {
		report.Render(stdout)
		if span != nil {
			fmt.Fprintf(stdout, "run span %s opened (heavyq run end when done)\n", span.ID)
		}
		if outputMode(stderr).Terse() {
			fmt.Fprintln(stdout, "next: "+preflightNext(report))
		}
	}
	if report.Verdict != preflight.Go {
		return 1
	}
	return 0
}

// runPreflight gathers the facts, runs every check and builds the report.
func runPreflight(cfg *config.Config, set *itemSet, common string, hours float64, parallel int, now time.Time, stderr io.Writer) (preflight.Report, error) {
	recs, _, err := ledgerindex.Recent(history.DefaultPath(), now.Add(-ledgerWindow))
	if err != nil {
		fmt.Fprintln(stderr, "heavyq: ledger unreadable, so proof durations are unknown:", err)
		recs = nil
	}
	rows, err := set.rows(recs, now, false)
	if err != nil {
		return preflight.Report{}, err
	}
	facts, commands := itemFacts(set, rows, recs)
	var open []string
	for _, f := range facts {
		if f.Open() {
			open = append(open, f.ID)
		}
	}
	results := []preflight.Results{
		{Name: preflight.NameAnchor, Found: preflight.CheckAnchor(preflight.AnchorInput{Goal: set.file.Goal,
			AnchorPath: set.file.Path, Items: facts, Hours: hours, Parallel: parallel, LeaseTTL: lease.DefaultTTL})},
		{Name: preflight.NameDependencies, Found: preflight.CheckDependencies(facts)},
		{Name: preflight.NameEnvironment, Found: preflight.CheckEnvironment(environmentFacts(cfg, set.root, parallel))},
		permissionResults(cfg, set.root, commands),
	}
	q := preflight.CheckQuota(preflight.ReadQuota(common, now), hours)
	results = append(results, preflight.Results{Name: preflight.NameQuota, Found: q})
	return preflight.Build(preflight.Plan{Goal: set.file.Goal, Hours: hours, Parallel: parallel, Items: open}, results), nil
}

// leasedLanes is how many lanes hold a live lease now, else defaultParallel.
func leasedLanes(store *lease.Store, now time.Time) int {
	ls, err := store.List()
	if err != nil {
		return defaultParallel
	}
	lanes := map[string]bool{}
	for _, l := range ls {
		if !l.Stale && now.Before(l.Expires) {
			lanes[l.Lane] = true
		}
	}
	if len(lanes) == 0 {
		return defaultParallel
	}
	return len(lanes)
}

// openRunSpan writes the span of the run the report was made for.
func openRunSpan(common string, r preflight.Report, now time.Time) (runspan.Span, error) {
	host, _ := os.Hostname()
	s := runspan.Span{Goal: r.Goal, Items: r.Items, Parallel: r.Parallel, Host: host}
	if r.Hours > 0 {
		end := now.Add(time.Duration(r.Hours * float64(time.Hour)))
		s.ExpectedEnd = &end
	}
	return runspan.Open(common, s, now)
}

// preflightNext is the one command an agent should run after the report.
func preflightNext(r preflight.Report) string {
	for _, f := range r.Findings {
		if f.Severity == preflight.Block {
			return f.Fix
		}
	}
	return "heavyq item claim --next"
}
