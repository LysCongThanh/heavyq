package cli

import (
	"fmt"
	"strings"
)

// usageTagline is the first line of the usage text.
const usageTagline = "heavyq - a priority, resource-aware queue for heavy commands"

// usageText is `heavyq --help`, generated from the command table: every
// synopsis line in table order, the flags of the run form (the one that has no
// command word), and its notes. There is no hand-written list of commands to
// keep in step; `heavyq <command> --help` has the rest.
func usageText() string {
	var b strings.Builder
	b.WriteString(usageTagline + "\n\nusage:\n")
	for _, c := range commandTable() {
		for _, line := range c.Synopsis {
			fmt.Fprintf(&b, "  %s\n", line)
		}
	}
	b.WriteString("\nEvery command has its own usage: heavyq <command> --help (also heavyq help <command>).\n")
	if run, ok := commandByName("run"); ok {
		b.WriteString("\nrun flags:\n")
		writeFlagRows(&b, run.Flags)
		if run.Notes != "" {
			fmt.Fprintf(&b, "\n%s\n", run.Notes)
		}
	}
	return b.String()
}
