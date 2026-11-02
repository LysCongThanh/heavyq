package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/LysCongThanh/heavyq/internal/config"
	"github.com/LysCongThanh/heavyq/internal/steer"
)

const briefUsage = `usage: heavyq [--config FILE] brief [--json]

The thread of this repository for the asking lane, in at most 300 tokens: the goal, the lane's items and
their status, the next command, a red gate, items that need a human, and one machine-pressure line. It is what
the SessionStart hook (heavyq hook install claude --features brief) injects at startup, resume and after
compaction. See docs/steer.md.
`

// briefCommand implements `heavyq brief [--json]`.
func briefCommand(cfg *config.Config, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("brief", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { fmt.Fprint(stderr, briefUsage) }
	asJSON := fs.Bool("json", false, "machine-readable output (schema in docs/steer.md)")
	if err := fs.Parse(args); err != nil {
		return ExitUsage
	}
	if fs.NArg() != 0 {
		fs.Usage()
		return ExitUsage
	}
	wd, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(stderr, "heavyq:", err)
		return ExitUsage
	}
	facts, err := gatherSteerFacts(cfg, wd, itemLane(wd))
	if err != nil {
		fmt.Fprintln(stderr, "heavyq:", err)
		return ExitUsage
	}
	b := steer.BuildBrief(facts, steer.SessionState{})
	if *asJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		enc.SetEscapeHTML(false)
		if err := enc.Encode(b); err != nil {
			fmt.Fprintln(stderr, "heavyq:", err)
			return 1
		}
		return 0
	}
	if b.Empty() {
		fmt.Fprintln(stdout, "nothing to brief: no goal or items in the anchor (.heavyq/anchor.md, see docs/anchor.md) and no red gate")
		return 0
	}
	fmt.Fprint(stdout, b.Render())
	return 0
}
