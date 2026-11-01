package cli

import (
	"flag"
	"fmt"
	"io"

	"github.com/LysCongThanh/heavyq/internal/history"
)

// historyCommand prints the most recent runs.
func historyCommand(args []string, w, stderr io.Writer) int {
	fs := flag.NewFlagSet("history", flag.ContinueOnError)
	fs.SetOutput(stderr)
	n := fs.Int("n", 20, "number of runs to show")
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
	fmt.Fprintf(w, "%-19s  %-10s %-12s %-4s %8s %8s %4s %-9s  %s\n", "END", "MACHINE", "LANE", "PRIO", "WAIT", "RUN", "EXIT", "CACHE", "COMMAND")
	for _, r := range recs {
		fmt.Fprintf(w, "%-19s  %-10s %-12s p%-3d %7.0fs %7.0fs %4d %-9s  %s\n", r.End.Local().Format("2006-01-02 15:04:05"),
			r.Machine, r.Lane, r.Prio, r.WaitSecs, r.RunSecs, r.Exit, orDefault(r.Cache, "-"), r.Cmd)
	}
	return 0
}
