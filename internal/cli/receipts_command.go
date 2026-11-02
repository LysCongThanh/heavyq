package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"time"

	"github.com/LysCongThanh/heavyq/internal/history"
	"github.com/LysCongThanh/heavyq/internal/receipts"
)

// receiptsCommand prints what heavyq did over a window of the ledger: exact
// counts, or with --share a rounded card with no names in it.
func receiptsCommand(args []string, w, stderr io.Writer) int {
	fs := flag.NewFlagSet("receipts", flag.ContinueOnError)
	fs.SetOutput(stderr)
	day := fs.Bool("day", false, "the last 24 hours")
	week := fs.Bool("week", false, "the last 7 days (default)")
	month := fs.Bool("month", false, "the last 30 days")
	share := fs.Bool("share", false, "round into ranges and drop every name: safe to post")
	asJSON := fs.Bool("json", false, "machine-readable output")
	if err := fs.Parse(args); err != nil {
		return ExitUsage
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(stderr, "heavyq: receipts takes no arguments (got %q)\n", fs.Arg(0))
		return ExitUsage
	}
	var names []string
	for name, set := range map[string]bool{"day": *day, "week": *week, "month": *month} {
		if set {
			names = append(names, name)
		}
	}
	if len(names) > 1 {
		fmt.Fprintln(stderr, "heavyq: use only one of --day, --week, --month")
		return ExitUsage
	}
	name := ""
	if len(names) == 1 {
		name = names[0]
	}
	name, d, err := receipts.ParseWindow(name)
	if err != nil {
		fmt.Fprintln(stderr, "heavyq:", err)
		return ExitUsage
	}
	to := time.Now()
	recs, err := history.ReadTail(history.DefaultPath(), history.TailLimit{Since: to.Add(-d)})
	if err != nil {
		fmt.Fprintln(stderr, "heavyq:", err)
		return 1
	}
	r := receipts.Compute(recs, to.Add(-d), to)
	var out any = r
	text := r.Text(name)
	if *share {
		s := receipts.NewShare(r, version(), name)
		out, text = s, s.Text
	}
	if *asJSON {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		if err := enc.Encode(out); err != nil {
			fmt.Fprintln(stderr, "heavyq:", err)
			return 1
		}
		return 0
	}
	fmt.Fprint(w, text)
	return 0
}
