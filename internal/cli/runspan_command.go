package cli

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/LysCongThanh/heavyq/internal/gitutil"
	"github.com/LysCongThanh/heavyq/internal/runspan"
)

const runSpanUsage = `usage:
  heavyq run end [<id>]
  heavyq run ls [--json]
  heavyq run current [--json]

A run span is the plan of one long run, opened by 'heavyq preflight' (docs/preflight.md). end closes the
current one (or <id>); ls lists them, newest first; current prints the open one and exits 1 when there is none.
('heavyq run' is not the priority form: that is 'heavyq <1-9> -- <command>'.)
`

// runSpanCommand implements `heavyq run end|ls|current`. Any other word after
// "run" is what it always was, an unknown first argument.
func runSpanCommand(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || (args[0] != "end" && args[0] != "ls" && args[0] != "current") {
		fmt.Fprint(stderr, unknownCommandMessage())
		return ExitUsage
	}
	wd, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(stderr, "heavyq:", err)
		return ExitUsage
	}
	common, err := gitutil.CommonDir(wd)
	if err != nil {
		fmt.Fprintf(stderr, "heavyq: %s is not inside a git work tree\n", wd)
		return ExitUsage
	}
	fs := flag.NewFlagSet("run "+args[0], flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { fmt.Fprint(stderr, runSpanUsage) }
	asJSON := fs.Bool("json", false, "machine-readable output")
	if err := fs.Parse(args[1:]); err != nil {
		return ExitUsage
	}
	now := time.Now()
	switch args[0] {
	case "end":
		if fs.NArg() > 1 || *asJSON {
			fs.Usage()
			return ExitUsage
		}
		return runSpanEnd(common, fs.Arg(0), now, stdout, stderr)
	case "ls":
		return runSpanList(common, now, *asJSON, fs.NArg(), stdout, stderr)
	}
	return runSpanCurrent(common, now, *asJSON, fs.NArg(), stdout, stderr)
}

func runSpanEnd(common, id string, now time.Time, stdout, stderr io.Writer) int {
	s, err := runspan.End(common, id, now)
	switch {
	case errors.Is(err, runspan.ErrBadID):
		fmt.Fprintln(stderr, "heavyq:", err)
		return ExitUsage
	case err != nil && !errors.Is(err, runspan.ErrEnded):
		fmt.Fprintln(stderr, "heavyq:", err)
		return 1
	case err != nil: // already ended: say so, change nothing
		fmt.Fprintf(stdout, "run span %s was already ended\n", s.ID)
		return 0
	}
	fmt.Fprintf(stdout, "run span %s ended after %s\n", s.ID, now.Sub(s.Started).Round(time.Minute))
	return 0
}

func runSpanList(common string, now time.Time, asJSON bool, extra int, stdout, stderr io.Writer) int {
	if extra != 0 {
		fmt.Fprint(stderr, runSpanUsage)
		return ExitUsage
	}
	spans, err := runspan.List(common)
	if err != nil {
		fmt.Fprintln(stderr, "heavyq:", err)
		return 1
	}
	if asJSON {
		if spans == nil {
			spans = []runspan.Span{}
		}
		return encodeJSON(stdout, stderr, spans)
	}
	if len(spans) == 0 {
		fmt.Fprintln(stdout, "no run spans (heavyq preflight opens one)")
	}
	for _, s := range spans {
		fmt.Fprintf(stdout, "%-24s %-7s %s  %d items  %s\n", s.ID, spanState(s, now), s.Started.Local().Format("2006-01-02 15:04"),
			len(s.Items), oneLine(s.Goal, 60))
	}
	return 0
}

func runSpanCurrent(common string, now time.Time, asJSON bool, extra int, stdout, stderr io.Writer) int {
	if extra != 0 {
		fmt.Fprint(stderr, runSpanUsage)
		return ExitUsage
	}
	cur, err := runspan.Current(common, now)
	if err != nil {
		fmt.Fprintln(stderr, "heavyq:", err)
		return 1
	}
	if asJSON {
		if rc := encodeJSON(stdout, stderr, cur); rc != 0 {
			return rc
		}
	} else if cur != nil {
		end := "no end stated"
		if cur.ExpectedEnd != nil {
			end = "expected end " + cur.ExpectedEnd.Local().Format("15:04")
		}
		fmt.Fprintf(stdout, "%s  started %s, %s, %d lanes\ngoal: %s\nitems: %s\n", cur.ID, cur.Started.Local().Format("15:04"), end,
			cur.Parallel, oneLine(cur.Goal, 100), oneLine(strings.Join(cur.Items, ", "), 200))
	} else {
		fmt.Fprintln(stdout, "no open run span (heavyq preflight opens one)")
	}
	if cur == nil {
		return 1
	}
	return 0
}

// spanState is open, lapsed (open but long past its end) or ended.
func spanState(s runspan.Span, now time.Time) string {
	switch {
	case !s.Open():
		return "ended"
	case s.Lapsed(now):
		return "lapsed"
	}
	return "open"
}

// encodeJSON writes v as indented JSON (a nil pointer is "null").
func encodeJSON(stdout, stderr io.Writer, v any) int {
	enc := json.NewEncoder(stdout)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		fmt.Fprintln(stderr, "heavyq:", err)
		return 1
	}
	return 0
}
