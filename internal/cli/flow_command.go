package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/LysCongThanh/heavyq/internal/config"
	"github.com/LysCongThanh/heavyq/internal/flow"
	"github.com/LysCongThanh/heavyq/internal/flowstate"
	"github.com/LysCongThanh/heavyq/internal/gitutil"
)

const flowUsage = `usage:
  heavyq [--config FILE] flow show [--json]
  heavyq [--config FILE] flow refresh
`

// flowCommand is `heavyq flow show|refresh`.
func flowCommand(cfg *config.Config, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, flowUsage)
		return ExitUsage
	}
	loadCfg := func() (*config.Config, error) { return cfg, nil }
	wd, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(stderr, "heavyq:", err)
		return 1
	}
	switch args[0] {
	case "refresh":
		if len(args) != 1 {
			fmt.Fprint(stderr, flowUsage)
			return ExitUsage
		}
		if _, err := refreshFlow(loadCfg, wd, true); err != nil {
			fmt.Fprintln(stderr, "heavyq:", flowErr(err))
			return 1
		}
		fmt.Fprintln(stdout, "flow state updated")
		return 0
	case "show":
		fs := flag.NewFlagSet("flow show", flag.ContinueOnError)
		fs.SetOutput(stderr)
		fs.Usage = func() { fmt.Fprint(stderr, flowUsage) }
		asJSON := fs.Bool("json", false, "the state document itself")
		if err := fs.Parse(args[1:]); err != nil || fs.NArg() != 0 {
			fs.Usage()
			return ExitUsage
		}
		return flowShow(loadCfg, wd, *asJSON, stdout, stderr)
	}
	fmt.Fprint(stderr, flowUsage)
	return ExitUsage
}

func flowErr(err error) error {
	if strings.Contains(err.Error(), errNotRepo.Error()) {
		return fmt.Errorf("not inside a git repository: flow state lives in <git common dir>/heavyq/flow")
	}
	return err
}

// flowShow prints the materialised state. It only reads; a repository with no
// state yet gets one computed, once.
func flowShow(loadCfg func() (*config.Config, error), wd string, asJSON bool, stdout, stderr io.Writer) int {
	common, err := gitutil.CommonDir(wd)
	if err != nil {
		fmt.Fprintln(stderr, "heavyq: not inside a git repository: flow state lives in <git common dir>/heavyq/flow")
		return 1
	}
	snap, err := flowstate.Read(common)
	if err == nil && snap == nil {
		if _, err = refreshFlow(loadCfg, wd, true); err == nil {
			snap, err = flowstate.Read(common)
		}
	}
	if err != nil || snap == nil {
		fmt.Fprintln(stderr, "heavyq:", err)
		return 1
	}
	if asJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(snap); err != nil {
			fmt.Fprintln(stderr, "heavyq:", err)
			return 1
		}
		return 0
	}
	writeFlowText(stdout, snap, time.Now())
	return 0
}

// writeFlowText is the human form: a handful of lines, the needs-you list
// first because it is the part that asks something of the reader.
func writeFlowText(w io.Writer, s *flow.Snapshot, now time.Time) {
	fmt.Fprintf(w, "flow  %s  updated %s ago\n", s.Repo, ago(now.Sub(s.Updated)))
	if s.Goal != "" {
		fmt.Fprintf(w, "goal: %s\n", s.Goal)
	}
	it := s.Items
	fmt.Fprintf(w, "items: %d/%d proven, %d running, %d blocked, %d claimed-unproven, %d done-stale, %d open\n",
		it.Proven, it.Total, it.Running, it.Blocked, it.ClaimedUnproven, it.DoneStale, it.Open)
	if len(s.NeedsYou) == 0 {
		fmt.Fprintln(w, "needs you: nothing")
	} else {
		fmt.Fprintf(w, "needs you (%d):\n", len(s.NeedsYou))
		for _, n := range s.NeedsYou {
			who := n.Lane
			if n.Item != "" {
				who = n.Item + " (" + n.Lane + ")"
			}
			fmt.Fprintf(w, "  %s  %s  for %s\n", who, n.Reason, ago(now.Sub(n.Since)))
		}
	}
	if c := s.Constraint; c.State != "" {
		sig := ""
		if c.Significant {
			sig = " (significant)"
		}
		fmt.Fprintf(w, "constraint: %s %.0f%% of lead time%s\n", c.State, c.Share*100, sig)
	}
	tp := s.Throughput
	lead := "n/a (no item claimed and then proven)" // the median needs claim-to-proof pairs; the count does not
	if tp.P50ClaimToProofS > 0 {
		lead = ago(time.Duration(tp.P50ClaimToProofS * float64(time.Second)))
	}
	fmt.Fprintf(w, "throughput: %.1f proven/h, median claim to proof %s, %d proven in the last %s\n",
		tp.ProvenPerHour, lead, tp.N, ago(time.Duration(tp.WindowS*float64(time.Second))))
	for _, l := range s.Lanes {
		item := ""
		if l.Item != "" {
			item = " " + l.Item
		}
		fmt.Fprintf(w, "lane %s: %s%s, %s\n", l.Lane, l.State, item, ago(now.Sub(l.Since)))
	}
	for _, e := range s.Events {
		fmt.Fprintf(w, "event %s %s lane %s: %s\n", e.At.Local().Format("15:04"), e.Kind, e.Lane, e.Detail)
	}
	if se := s.Session; se != nil {
		line := "session: " + se.Model
		if se.AgentVersion != "" {
			line += " (claude-code " + se.AgentVersion + ")"
		}
		if r := se.RateLimits; r != nil {
			if r.FiveHourPct != nil {
				line += fmt.Sprintf(", 5h limit %.0f%%", *r.FiveHourPct)
			}
			if r.SevenDayPct != nil {
				line += fmt.Sprintf(", 7d limit %.0f%%", *r.SevenDayPct)
			}
		}
		fmt.Fprintln(w, line)
	}
}

// ago is a duration in its two most significant units: 2h05m, 14m, 40s.
func ago(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	switch {
	case d >= time.Hour:
		return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
	case d >= time.Minute:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	return fmt.Sprintf("%ds", int(d.Seconds()))
}
