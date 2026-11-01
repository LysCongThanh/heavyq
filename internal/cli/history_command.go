package cli

import (
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/LysCongThanh/heavyq/internal/history"
)

// historyCommand prints the most recent runs.
func historyCommand(args []string, w, stderr io.Writer) int {
	fs := flag.NewFlagSet("history", flag.ContinueOnError)
	fs.SetOutput(stderr)
	n := fs.Int("n", 20, "number of runs to show")
	verbose := fs.Bool("v", false, "also show the tree, session, item and gate of each run")
	if err := fs.Parse(args); err != nil {
		return ExitUsage
	}
	path := history.DefaultPath()
	recs, err := history.ReadLast(path, *n)
	if err != nil {
		fmt.Fprintln(stderr, "heavyq:", err)
		return 1
	}
	if len(recs) == 0 {
		fmt.Fprintf(w, "no runs recorded yet (%s)\n", path)
		return 0
	}
	fmt.Fprintf(w, "%-19s  %-10s %-12s %-4s %8s %8s %4s %-9s %-9s  %s\n", "END", "MACHINE", "LANE", "PRIO", "WAIT", "RUN", "EXIT", "CACHE", "COMMIT", "COMMAND")
	for _, r := range recs {
		fmt.Fprintf(w, "%-19s  %-10s %-12s p%-3d %7.0fs %7.0fs %4d %-9s %-9s  %s\n", r.End.Local().Format("2006-01-02 15:04:05"),
			r.Machine, r.Lane, r.Prio, r.WaitSecs, r.RunSecs, r.Exit, orDefault(r.Cache, "-"), commitLabel(r), r.Cmd)
		if d := recordDetail(r); *verbose && d != "" {
			fmt.Fprintf(w, "    %s\n", d)
		}
	}
	return 0
}

// commitLabel is the short commit of a run, "+" after it when the work tree
// was dirty, "-" when the run has no code identity (older lines, no git).
func commitLabel(r history.Record) string {
	if r.Commit == "" {
		return "-"
	}
	label := r.Commit[:min(8, len(r.Commit))]
	if r.Dirty {
		label += "+"
	}
	return label
}

// recordDetail lists the identity fields of r that are set, for `history -v`.
func recordDetail(r history.Record) string {
	var parts []string
	add := func(k, v string) {
		if v != "" {
			parts = append(parts, k+" "+v)
		}
	}
	if r.Tree != "" {
		add("tree", r.Tree[:min(12, len(r.Tree))]+treeSuffix(r))
	}
	add("session", r.Session)
	add("item", r.Item)
	add("gate", r.Gate)
	return strings.Join(parts, "  ")
}

// treeSuffix marks an edited tree: its id carries the digest of the edits
// after a "+", which the short form would cut off.
func treeSuffix(r history.Record) string {
	if r.Dirty {
		return "+edits"
	}
	return ""
}
