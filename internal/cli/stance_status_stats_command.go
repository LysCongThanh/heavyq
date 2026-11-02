package cli

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/LysCongThanh/heavyq/internal/stance"
)

func stanceStatus(env stance.Env, cwd, lane string, asJSON bool, stdout, stderr io.Writer) int {
	st, err := env.Status(cwd, lane)
	if err != nil {
		return stanceFail(stderr, err)
	}
	if asJSON {
		return writeStanceJSON(st, stdout, stderr)
	}
	switch {
	case st.Active != nil:
		a := st.Active
		line := fmt.Sprintf("lane %s: stance %s since %s (%d min", lane, a.Stance, a.Since.Local().Format("15:04"), st.Minutes)
		if !a.Expires.IsZero() {
			line += fmt.Sprintf(", %d min left in the time box", st.LeftMinutes)
		}
		fmt.Fprintln(stdout, line+")")
		fmt.Fprintf(stdout, "  why: %s [%s]\n", a.Why, a.Trigger)
		if r := st.Record; r != nil {
			fmt.Fprintf(stdout, "  decision record: %s\n", recordLine(r))
		}
	case st.Muted:
		fmt.Fprintf(stdout, "lane %s: stances off (heavyq stance enter <name> turns them on)\n", lane)
	default:
		fmt.Fprintf(stdout, "lane %s: no stance\n", lane)
	}
	e := st.Evidence
	fmt.Fprintf(stdout, "evidence: same failure on %d trees", e.FailureTrees)
	if e.FailureClass != "" {
		fmt.Fprintf(stdout, " (%s)", e.FailureClass)
	}
	fmt.Fprintf(stdout, "; %d reverts, %d rewrites without a proof\n", e.ChurnReverts, e.ChurnRewrite)
	if len(st.Untrusted) > 0 {
		fmt.Fprintf(stdout, "untrusted repo stances (listed, never injected): %s; read .heavyq/stances, then: heavyq trust\n",
			strings.Join(st.Untrusted, ", "))
	}
	return 0
}

func recordLine(r *stance.RecordStatus) string {
	switch {
	case !r.Exists:
		return r.Path + " (missing)"
	case len(r.Problems) > 0:
		return r.Path + " (incomplete: " + strings.Join(r.Problems, "; ") + ")"
	}
	return r.Path + " (complete)"
}

func stanceStats(env stance.Env, cwd string, asJSON bool, stdout, stderr io.Writer) int {
	stats, err := env.Stats(cwd)
	if err != nil {
		return stanceFail(stderr, err)
	}
	if asJSON {
		return writeStanceJSON(stats, stdout, stderr)
	}
	if len(stats) == 0 {
		fmt.Fprintln(stdout, "no stance activations yet")
		return 0
	}
	tw := tabwriter.NewWriter(stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "STANCE\tACTIVATIONS\tPROVEN\tTO PROOF (median)\tLANE BASELINE (median)\tENTERED BY\tENDED BY")
	for _, s := range stats {
		fmt.Fprintf(tw, "%s\t%d\t%d\t%s\t%s\t%s\t%s\n", s.Stance, s.Activations, s.Proven,
			proofCell(s.Proven, s.MedProofSecs, s.MedProofRuns), proofCell(s.Baselined, s.MedBaseSecs, s.MedBaseRuns),
			counts(s.ByTrigger), counts(s.ByExit))
	}
	if err := tw.Flush(); err != nil {
		return stanceFail(stderr, err)
	}
	return 0
}

// proofCell is "12m, 4 runs", or "-" when nothing was measured.
func proofCell(n int, secs, runs float64) string {
	if n == 0 {
		return "-"
	}
	return fmt.Sprintf("%s, %.0f runs", (time.Duration(secs) * time.Second).Round(time.Second), runs)
}

func counts(m map[string]int) string {
	if len(m) == 0 {
		return "-"
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = fmt.Sprintf("%s %d", k, m[k])
	}
	return strings.Join(parts, ", ")
}
