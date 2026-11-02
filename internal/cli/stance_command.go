package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/LysCongThanh/heavyq/internal/stance"
)

const stanceUsage = `usage:
  heavyq stance ls [--json]
  heavyq stance show <name> [--json]
  heavyq stance enter <name> [--item ID] [--why TEXT]   put this lane into a stance and print its compact form
  heavyq stance exit [--item ID] [--force]              leave it (a decision record may be required)
  heavyq stance off                                     leave it and mute stances for this lane until the next enter
  heavyq stance status [--json]                         the lane's stance, its decision record and the evidence now
  heavyq stance stats [--json]                          time and runs to proof after each stance, against the lane's baseline
  heavyq trust [--list|--revoke]                        trust this repository's own stances (a content hash)

Stances are short documents describing a way of thinking. Sources, highest
precedence first: <repo>/.heavyq/stances/*.md (only once trusted), the user's stances
directory ($XDG_CONFIG_HOME/heavyq/stances or ~/.config/heavyq/stances), built-in.
HEAVYQ_STANCES=off turns every stance off for a process. See docs/stances.md.
`

// stanceListEntry is one element of `stance ls --json`.
type stanceListEntry struct {
	Name         string `json:"name"`
	Source       string `json:"source"`
	Path         string `json:"path"`
	Description  string `json:"description"`
	InjectTokens int    `json:"inject_tokens"`
	// Untrusted: from a repository whose stances were not trusted; listed, never injected.
	Untrusted bool `json:"untrusted,omitempty"`
}

// stanceCommand implements `heavyq stance ...`. ls and show are read-only views
// of the stances in effect for the current directory; enter, exit, off, status
// and stats act on the lane (stance_lane_command.go). None needs a config
// file, so they work even when the config is broken.
func stanceCommand(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, stanceUsage)
		return ExitUsage
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "enter", "exit", "off", "status", "stats":
		return stanceLaneCommand(sub, rest, stdout, stderr)
	case "ls", "show":
	default:
		fmt.Fprintf(stderr, "heavyq: unknown stance command %q (ls, show, enter, exit, off, status, stats)\n\n%s", sub, stanceUsage)
		return ExitUsage
	}
	fs := flag.NewFlagSet("stance "+sub, flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { fmt.Fprint(stderr, stanceUsage) }
	asJSON := fs.Bool("json", false, "machine-readable output")
	var names []string
	for { // flags and the name may come in any order
		if err := fs.Parse(rest); err != nil {
			return ExitUsage
		}
		if fs.NArg() == 0 {
			break
		}
		names = append(names, fs.Arg(0))
		rest = fs.Args()[1:]
	}
	if (sub == "ls" && len(names) != 0) || (sub == "show" && len(names) != 1) {
		fs.Usage()
		return ExitUsage
	}
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(stderr, "heavyq:", err)
		return ExitUsage
	}
	set, err := stance.DefaultEnv(cwd).LoadSet(cwd)
	all := set.All
	if err != nil {
		fmt.Fprintln(stderr, "heavyq: invalid stance file(s):")
		fmt.Fprintln(stderr, err)
		return ExitUsage
	}
	if sub == "ls" {
		return stanceList(set, *asJSON, stdout, stderr)
	}
	return stanceShow(all, names[0], *asJSON, stdout, stderr)
}

func stanceList(set stance.Set, asJSON bool, stdout, stderr io.Writer) int {
	all := set.All
	if asJSON {
		out := make([]stanceListEntry, len(all))
		for i, s := range all {
			out[i] = stanceListEntry{s.Name, string(s.Source), s.Path, s.Description, s.InjectTokens, set.Untrusted(s)}
		}
		return writeStanceJSON(out, stdout, stderr)
	}
	tw := tabwriter.NewWriter(stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "NAME\tSOURCE\tDESCRIPTION")
	for _, s := range all {
		src := string(s.Source)
		if s.Source != stance.SourceBuiltin {
			src += " " + s.Path
		}
		if set.Untrusted(s) {
			src += " (untrusted: run heavyq trust)"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\n", s.Name, src, s.Description)
	}
	if err := tw.Flush(); err != nil {
		fmt.Fprintln(stderr, "heavyq:", err)
		return 1
	}
	return 0
}

func stanceShow(all []stance.Stance, name string, asJSON bool, stdout, stderr io.Writer) int {
	s, ok := stance.Find(all, name)
	if !ok {
		known := make([]string, len(all))
		for i, a := range all {
			known[i] = a.Name
		}
		fmt.Fprintf(stderr, "heavyq: no stance named %q (known: %s)\n", name, strings.Join(known, ", "))
		return ExitUsage
	}
	if asJSON {
		return writeStanceJSON(s, stdout, stderr)
	}
	fmt.Fprintln(stdout, s.Body)
	return 0
}

func writeStanceJSON(v any, stdout, stderr io.Writer) int {
	enc := json.NewEncoder(stdout)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false) // bodies are prose, keep "<" and "&" readable
	if err := enc.Encode(v); err != nil {
		fmt.Fprintln(stderr, "heavyq:", err)
		return 1
	}
	return 0
}
