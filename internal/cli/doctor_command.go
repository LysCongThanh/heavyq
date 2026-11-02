package cli

import (
	"flag"
	"fmt"
	"io"
	"time"

	"github.com/LysCongThanh/heavyq/internal/history"
	"github.com/LysCongThanh/heavyq/internal/ledgerindex"
)

// doctorCommand implements `heavyq doctor [--reindex]`: health checks of what
// heavyq keeps on disk. For now that is the ledger index (docs/ledger.md): its
// state and size, and with --reindex a rebuild from the ledger. Exit 0 when the
// index is fresh (or was rebuilt), 1 when it is stale or missing, 2 on usage.
func doctorCommand(args []string, stdout, stderr io.Writer) (rc int) {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	fs.SetOutput(stderr)
	reindex := fs.Bool("reindex", false, "rebuild the ledger index from the ledger")
	fs.Usage = func() { fmt.Fprintln(stderr, "usage: heavyq doctor [--reindex]") }
	if err := fs.Parse(args); err != nil {
		return ExitUsage
	}
	if fs.NArg() != 0 {
		fs.Usage()
		return ExitUsage
	}
	defer func() {
		if rc != ExitUsage {
			printBriefDelivery(stdout) // informational: never changes the exit code
		}
	}()
	path := history.DefaultPath()
	if path == "" {
		fmt.Fprintln(stderr, "heavyq: no state directory (set $HEAVYQ_HISTORY or $XDG_STATE_HOME)")
		return ExitUsage
	}
	if *reindex {
		st, took, err := ledgerindex.Reindex(path)
		if err != nil {
			fmt.Fprintln(stderr, "heavyq: reindex failed:", err)
			return 1
		}
		fmt.Fprintf(stdout, "reindexed %d records in %s\n", st.Records, took.Round(time.Millisecond))
	}
	st, err := ledgerindex.StatsOf(path)
	if err != nil {
		fmt.Fprintln(stderr, "heavyq: index unreadable:", err)
		return 1
	}
	if files, _ := history.Files(path); st.State == "missing" && len(files) == 0 {
		fmt.Fprintf(stdout, "ledger index: nothing to index, no runs recorded yet (%s)\n", path)
		return 0
	}
	fmt.Fprintf(stdout, "ledger index: %s\n  dir:      %s\n  lanes:    %d\n  trees:    %d (evicted: %t)\n  records:  %d\n  size:     %s\n",
		st.State, st.Dir, st.Lanes, st.Trees, st.Evicted, st.Records, humanBytes(st.Bytes))
	if st.State != "fresh" {
		fmt.Fprintln(stdout, "  hot paths read the ledger tail instead; run: heavyq doctor --reindex")
		return 1
	}
	return 0
}
