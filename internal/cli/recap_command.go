package cli

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/LysCongThanh/heavyq/internal/config"
	"github.com/LysCongThanh/heavyq/internal/flow"
	"github.com/LysCongThanh/heavyq/internal/flowstate"
	"github.com/LysCongThanh/heavyq/internal/flowview"
	"github.com/LysCongThanh/heavyq/internal/gitutil"
	"github.com/LysCongThanh/heavyq/internal/notify"
	"github.com/LysCongThanh/heavyq/internal/recap"
	"github.com/LysCongThanh/heavyq/internal/tui/render"
)

const recapUsage = `usage: heavyq [--config FILE] recap [--since 2h] [--session S] [--run ID] [--json] [--verbose] [--ascii] [--no-notify]

How it is going and what landed, in one screen: a header (progress, forecast, the current constraint), the one
best next action, then merged-proven / proven / claimed-unproven / blocked / wasted / in-flight. Every line ends
with the command that acts on it. --since takes a duration such as 90m or 2h (default: the run's start, else 4h).
See docs/recap.md.
`

// defaultRecapWindow is how far back a recap looks when no run says otherwise.
const defaultRecapWindow = 4 * time.Hour

// recapCommand implements `heavyq recap`.
func recapCommand(cfg *config.Config, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("recap", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { fmt.Fprint(stderr, recapUsage) }
	sinceArg := fs.String("since", "", "how far back to look, e.g. 90m or 2h (default: the run's start, else 4h)")
	session := fs.String("session", "", "only this session's items and runs")
	runID := fs.String("run", "", "the run declared by preflight: look back to its start")
	asJSON := fs.Bool("json", false, "machine-readable output (schema in docs/recap.md)")
	verbose := fs.Bool("verbose", false, "also lanes, and the time of each line")
	ascii := fs.Bool("ascii", false, "ASCII glyphs and text only")
	noNotify := fs.Bool("no-notify", false, "do not push a notification on a state change")
	if err := fs.Parse(args); err != nil {
		return ExitUsage
	}
	if fs.NArg() != 0 || (*sinceArg != "" && *runID != "") {
		fs.Usage()
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
	now := time.Now()
	common, _ := gitutil.CommonDir(set.root)
	state := readFlowState(common, stderr)

	since, rc := recapSince(*sinceArg, *runID, state, now, stderr)
	if rc != 0 {
		return rc
	}
	recs := ledgerOrNone(stderr)
	rows, err := set.rows(recs, now, false)
	if err != nil {
		fmt.Fprintln(stderr, "heavyq:", err)
		return 1
	}
	facts := recap.Facts{Now: now, Since: since, Goal: set.file.Goal, Session: *session, State: state,
		Items: recapItems(rows), Records: recordsOfRepo(recs, set.subject(set.root).Repo)}
	if ref, ok := gitutil.DefaultBranchRef(set.root); ok {
		facts.MergeRef = ref
		facts.Merged = func(commit string) bool { return gitutil.IsAncestor(set.root, commit, ref) }
	}
	r := recap.Build(facts)

	if *asJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		enc.SetEscapeHTML(false)
		if err := enc.Encode(r); err != nil {
			fmt.Fprintln(stderr, "heavyq:", err)
			return 1
		}
	} else {
		th := render.ThemeFromEnv(os.Getenv, *ascii)
		fmt.Fprint(stdout, recap.Render(r, recap.RenderOptions{ASCII: th.ASCII, Verbose: *verbose}))
	}
	if !*noNotify && state != nil && common != "" {
		env, goos := notify.SystemEnv()
		notify.Check(common, flowview.NotifyFacts(state, now), notify.Options{Budget: notify.BudgetFromEnv(env),
			Channels: func() []notify.Channel { return notify.DefaultChannels(env, goos) }})
	}
	return 0
}

// readFlowState reads the materialised state; nil when there is none. A state
// that is there but unreadable is reported, since the recap is not a hot path.
func readFlowState(common string, stderr io.Writer) *flow.Snapshot {
	if common == "" {
		return nil
	}
	st, err := flowstate.ReadFile(flowstate.StatePath(common))
	switch {
	case err == nil:
		return st
	case !errors.Is(err, flowstate.ErrNoState):
		fmt.Fprintln(stderr, "heavyq: ignoring the flow state:", err)
	}
	return nil
}

// recapSince resolves the start of the window: --since, else --run's start,
// else the declared run's start, else defaultRecapWindow back.
func recapSince(sinceArg, runID string, st *flow.Snapshot, now time.Time, stderr io.Writer) (time.Time, int) {
	if sinceArg != "" {
		d, err := time.ParseDuration(sinceArg)
		if err != nil || d <= 0 {
			fmt.Fprintf(stderr, "heavyq: --since %q: want a positive duration such as 90m or 2h\n", sinceArg)
			return time.Time{}, ExitUsage
		}
		return now.Add(-d), 0
	}
	var run *flow.RunInfo
	if st != nil {
		run = st.RunInfo()
	}
	if runID != "" && (run == nil || run.ID != runID) {
		fmt.Fprintf(stderr, "heavyq: no run %q is declared in the flow state (runs come from heavyq preflight); use --since\n", runID)
		return time.Time{}, 1
	}
	if run != nil && !run.Started.IsZero() && run.Started.Before(now) {
		return run.Started, 0
	}
	return now.Add(-defaultRecapWindow), 0
}
