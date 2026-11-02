package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"text/tabwriter"
	"time"

	"github.com/LysCongThanh/heavyq/internal/config"
	"github.com/LysCongThanh/heavyq/internal/history"
	"github.com/LysCongThanh/heavyq/internal/ledgerindex"
	"github.com/LysCongThanh/heavyq/internal/mentor"
)

const mentorUsage = `usage:
  heavyq mentor stats [--json]
  heavyq mentor mute <rule>
  heavyq mentor unmute <rule>

The mentor adds one short note after a repeated identical failure (hook
claude-posttooluse), to 80% of eligible events; the other 20% are logged and left
silent, so the effect can be measured: did the next heavy run differ within 3 runs.
"stats" shows each rule's two arms and its lift. A rule with under 10 points of lift
after 200 resolved events per arm mutes itself ([mentor] auto_mute). Rules: ` + mentor.RuleRepeatFailure + `.
HEAVYQ_MENTOR=off silences the mentor for one process tree. See docs/mentor.md.
`

// mentorCommand implements `heavyq mentor stats|mute|unmute`.
func mentorCommand(cfgPath string, explicit bool, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, mentorUsage)
		return ExitUsage
	}
	cfg, err := config.Load(cfgPath, explicit)
	if err != nil {
		fmt.Fprintln(stderr, "heavyq:", err)
		return ExitUsage
	}
	store, ledger, err := mentorStore()
	if err != nil {
		fmt.Fprintln(stderr, "heavyq:", err)
		return 1
	}
	switch args[0] {
	case "stats":
		return mentorStats(cfg, store, ledger, args[1:], stdout, stderr)
	case "mute", "unmute":
		if len(args) != 2 {
			fmt.Fprint(stderr, mentorUsage)
			return ExitUsage
		}
		return mentorSetMute(store, args[1], args[0] == "mute", stdout, stderr)
	}
	fmt.Fprintf(stderr, "heavyq: unknown mentor command %q (stats, mute, unmute)\n\n%s", args[0], mentorUsage)
	return ExitUsage
}

func mentorSetMute(store *mentor.Store, rule string, mute bool, stdout, stderr io.Writer) int {
	if err := store.SetMute(rule, mute, mentor.MutedByUser, "", time.Now()); err != nil {
		fmt.Fprintln(stderr, "heavyq:", err)
		if !mentor.KnownRule(rule) {
			return ExitUsage
		}
		return 1
	}
	verb := "unmuted"
	if mute {
		verb = "muted"
	}
	fmt.Fprintf(stdout, "%s %s\n", verb, rule)
	return 0
}

// mentorStatsJSON is the `mentor stats --json` schema.
type mentorStatsJSON struct {
	Rules []mentor.RuleStats `json:"rules"`
}

func mentorStats(cfg *config.Config, store *mentor.Store, ledger string, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("mentor stats", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { fmt.Fprint(stderr, mentorUsage) }
	asJSON := fs.Bool("json", false, "machine-readable output")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		return ExitUsage
	}
	// Settle what the lanes' records can settle first, so the numbers are current.
	laneRecords := func(lane string) ([]history.Record, error) {
		recs, _, err := ledgerindex.LaneRuns(ledger, lane)
		return recs, err
	}
	if _, err := store.SweepPending(laneRecords, mentorConfig(cfg), time.Now()); err != nil {
		fmt.Fprintln(stderr, "heavyq: warning: pending outcomes not settled:", err)
	}
	stats, err := store.StatsWithMutes()
	if err != nil {
		fmt.Fprintln(stderr, "heavyq:", err)
		return 1
	}
	if *asJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(mentorStatsJSON{Rules: stats}); err != nil {
			fmt.Fprintln(stderr, "heavyq:", err)
			return 1
		}
		return 0
	}
	writeMentorTable(stdout, stats, mentorConfig(cfg))
	return 0
}

func writeMentorTable(w io.Writer, stats []mentor.RuleStats, c mentor.Config) {
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "RULE\tARM\tEVENTS\tRESOLVED\tDIFFERED\tRATE")
	for _, rs := range stats {
		for _, a := range []struct {
			name string
			s    mentor.ArmStats
		}{{"inject", rs.Inject}, {"holdout", rs.Holdout}} {
			fmt.Fprintf(tw, "%s\t%s\t%d\t%d\t%d\t%.0f%%\n", rs.Rule, a.name, a.s.Events, a.s.Resolved, a.s.Differed, a.s.Rate*100)
		}
	}
	_ = tw.Flush()
	for _, rs := range stats {
		lift := "n/a (needs resolved events in both arms)"
		if rs.LiftPP != nil {
			lift = fmt.Sprintf("%+.1f pp", *rs.LiftPP)
		}
		fmt.Fprintf(w, "%s: lift %s, holdout %d%%", rs.Rule, lift, c.HoldoutPct)
		if c.AutoMute.Enabled {
			fmt.Fprintf(w, ", auto-mute below %.0f pp after %d resolved per arm", c.AutoMute.MinLiftPP, c.AutoMute.MinEvents)
		}
		if rs.Muted {
			fmt.Fprintf(w, ", MUTED by %s", rs.MutedBy)
			if rs.MutedWhy != "" {
				fmt.Fprintf(w, " (%s)", rs.MutedWhy)
			}
		}
		fmt.Fprintln(w)
	}
}
