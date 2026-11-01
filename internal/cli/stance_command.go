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

Stances are short documents describing a way of thinking. Sources, highest
precedence first: <repo>/.heavyq/stances/*.md, the user's stances directory
($XDG_CONFIG_HOME/heavyq/stances or ~/.config/heavyq/stances), built-in.
`

// stanceListEntry is one element of `stance ls --json`.
type stanceListEntry struct {
	Name         string `json:"name"`
	Source       string `json:"source"`
	Path         string `json:"path"`
	Description  string `json:"description"`
	InjectTokens int    `json:"inject_tokens"`
}

// stanceCommand implements `heavyq stance ls|show`: read-only views of the
// stances in effect for the current directory. It needs no config file, so
// it works even when the config is broken.
func stanceCommand(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, stanceUsage)
		return ExitUsage
	}
	sub, rest := args[0], args[1:]
	if sub != "ls" && sub != "show" {
		fmt.Fprintf(stderr, "heavyq: unknown stance command %q (ls, show)\n\n%s", sub, stanceUsage)
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
	all, err := stance.Load(stance.DefaultDirs(cwd))
	if err != nil {
		fmt.Fprintln(stderr, "heavyq: invalid stance file(s):")
		fmt.Fprintln(stderr, err)
		return ExitUsage
	}
	if sub == "ls" {
		return stanceList(all, *asJSON, stdout, stderr)
	}
	return stanceShow(all, names[0], *asJSON, stdout, stderr)
}

func stanceList(all []stance.Stance, asJSON bool, stdout, stderr io.Writer) int {
	if asJSON {
		out := make([]stanceListEntry, len(all))
		for i, s := range all {
			out[i] = stanceListEntry{s.Name, string(s.Source), s.Path, s.Description, s.InjectTokens}
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
