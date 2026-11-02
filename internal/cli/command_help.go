package cli

import (
	"fmt"
	"io"
	"strings"
)

// docsURL is where the docs/ files a help block points to live.
const docsURL = "https://github.com/LysCongThanh/heavyq/blob/main/docs/"

// wantsCommandHelp reports whether args (after any --config FILE) ask for the
// help of a command: a known command first, and -h or --help among the words
// before a "--" (what follows "--" is the user's own command, never ours).
func wantsCommandHelp(args []string) (commandSpec, bool) {
	if len(args) < 2 {
		return commandSpec{}, false
	}
	c, ok := commandByName(args[0])
	if !ok {
		return commandSpec{}, false
	}
	for _, a := range args[1:] {
		switch a {
		case "--":
			return commandSpec{}, false
		case "-h", "--help", "-help":
			return c, true
		}
	}
	return commandSpec{}, false
}

// writeCommandHelp prints the usage block of one command: what it does, how
// to call it, its flags and a pointer to the docs. Flag defaults are not the
// Go flag package's; the text is the table's.
func writeCommandHelp(w io.Writer, c commandSpec) {
	title := c.Name
	if c.Name == "run" {
		title = "<prio 1-9>"
	}
	fmt.Fprintf(w, "heavyq %s: %s\n\nusage:\n", title, c.Summary)
	for _, line := range c.Synopsis {
		fmt.Fprintf(w, "  %s\n", line)
	}
	if len(c.Subs) > 0 {
		fmt.Fprint(w, "\ncommands:\n")
		rows := make([][2]string, 0, len(c.Subs))
		for _, s := range c.Subs {
			rows = append(rows, [2]string{strings.TrimSpace(s.Name + " " + s.Args), s.Summary})
		}
		writeColumns(w, rows)
	}
	if len(c.Flags) > 0 {
		fmt.Fprint(w, "\nflags:\n")
		writeFlagRows(w, c.Flags)
	}
	for _, s := range c.Subs {
		if len(s.Flags) > 0 {
			fmt.Fprintf(w, "\nflags of %s %s:\n", c.Name, s.Name)
			writeFlagRows(w, s.Flags)
		}
	}
	if c.Notes != "" {
		fmt.Fprintf(w, "\n%s\n", c.Notes)
	}
	if c.Docs != "" {
		fmt.Fprintf(w, "\nmore: %s%s\n", docsURL, c.Docs)
	}
}

func writeFlagRows(w io.Writer, flags []flagSpec) {
	rows := make([][2]string, 0, len(flags))
	for _, f := range flags {
		rows = append(rows, [2]string{f.display(), f.Help})
	}
	writeColumns(w, rows)
}

// writeColumns prints rows as two aligned columns, indented by two spaces.
func writeColumns(w io.Writer, rows [][2]string) {
	width := 0
	for _, r := range rows {
		if len(r[0]) > width {
			width = len(r[0])
		}
	}
	for _, r := range rows {
		fmt.Fprintf(w, "  %-*s  %s\n", width, r[0], r[1])
	}
}
