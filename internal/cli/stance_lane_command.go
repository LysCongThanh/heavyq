package cli

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/LysCongThanh/heavyq/internal/stance"
)

// stanceLaneCommand implements the subcommands that act on the lane of the
// current directory: enter, exit, off, status, stats. Like ls and show, they
// need no config file.
func stanceLaneCommand(sub string, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("stance "+sub, flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { fmt.Fprint(stderr, stanceUsage) }
	item := fs.String("item", "", "item whose decision record applies (default: the lane's leased item)")
	why := fs.String("why", "", "the reason, shown as \"why now\" (enter)")
	force := fs.Bool("force", false, "leave even though the exit condition is not met (exit)")
	asJSON := fs.Bool("json", false, "machine-readable output (status, stats)")
	var pos []string
	for { // flags and the name may come in any order
		if err := fs.Parse(args); err != nil {
			return ExitUsage
		}
		if fs.NArg() == 0 {
			break
		}
		pos, args = append(pos, fs.Arg(0)), fs.Args()[1:]
	}
	want := 0
	if sub == "enter" {
		want = 1
	}
	if len(pos) != want {
		fs.Usage()
		return ExitUsage
	}
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(stderr, "heavyq:", err)
		return ExitUsage
	}
	env, lane := stance.DefaultEnv(cwd), itemLane(cwd)
	var rc int
	switch sub {
	case "enter":
		rc = stanceEnter(env, cwd, lane, pos[0], *item, *why, stdout, stderr)
	case "exit":
		rc = stanceExit(env, cwd, lane, *item, *force, stdout, stderr)
	case "off":
		rc = stanceOff(env, cwd, lane, stdout, stderr)
	case "status":
		rc = stanceStatus(env, cwd, lane, *asJSON, stdout, stderr)
	default:
		rc = stanceStats(env, cwd, *asJSON, stdout, stderr)
	}
	return rc
}

func stanceFail(stderr io.Writer, err error) int {
	fmt.Fprintln(stderr, "heavyq:", err)
	return 1
}

func stanceEnter(env stance.Env, cwd, lane, name, item, why string, stdout, stderr io.Writer) int {
	entry, err := env.EnterManual(cwd, lane, itemSession(lane), item, name, why)
	if err != nil {
		return stanceFail(stderr, err)
	}
	fmt.Fprintln(stdout, entry.Context)
	return 0
}

func stanceExit(env stance.Env, cwd, lane, item string, force bool, stdout, stderr io.Writer) int {
	res, err := env.Leave(cwd, lane, item, force)
	if err != nil {
		return stanceFail(stderr, err)
	}
	if res.Left {
		fmt.Fprintf(stdout, "left stance %s (%s)\n", res.Stance, strings.ReplaceAll(res.Reason, "_", " "))
		return 0
	}
	fmt.Fprintf(stderr, "heavyq: cannot leave stance %s yet:\n", res.Stance)
	for _, p := range res.Check.Problems {
		fmt.Fprintln(stderr, "  -", p)
	}
	if res.Record != "" {
		fmt.Fprintln(stderr, "  decision record:", res.Record)
	}
	fmt.Fprintln(stderr, "next: write the decision record (see docs/stances.md), or leave anyway with: heavyq stance exit --force")
	return 1
}

func stanceOff(env stance.Env, cwd, lane string, stdout, stderr io.Writer) int {
	if err := env.Off(cwd, lane, itemSession(lane)); err != nil {
		return stanceFail(stderr, err)
	}
	fmt.Fprintf(stdout, "stances are off for lane %s until: heavyq stance enter <name>\n", lane)
	return 0
}
