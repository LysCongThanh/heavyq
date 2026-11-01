package repoconfig

import (
	"fmt"
	"strings"
)

// fileHeader explains the file to whoever opens it next, human or agent: what
// it is, the trust model, and how to add a gate. The text before the first
// blank line is written by RenderFile, because it depends on what was found.
const fileHeader = `#
# ` + "`heavyq gate status`" + ` says whether the code checked out now has passed every command
# of a gate; ` + "`heavyq gate run`" + ` runs the commands that have not, through the heavyq
# queue, and records the results as proof.
#
# Trust model: this file is trusted like a Makefile. Whoever can edit it decides
# which commands run, so review changes to it like changes to CI. Its commands
# run only on an explicit ` + "`heavyq gate run`" + `: heavyq's hook never executes them, and
# heavyq never runs anything to work out what to write here.
#
# To add a gate, append another [[gate]] table with its own name:
#
#   [[gate]]
#   name = "smoke"          # [a-z0-9][a-z0-9_-]{0,39}, unique in this file
#   run  = ["make smoke"]   # shell strings, run in order, all must pass
#   prio = 2                # optional, 1-9, default 3 (1 runs first)
#   # light  = true         # optional: may use the light slots
#   # remote = "auto"       # optional: local (default) | auto | <remote name>
#
# then run it with ` + "`heavyq gate run smoke`" + `. Format: docs/gates.md in the heavyq repository.

`

// RenderFile returns the text of a heavyq.toml for the gate that Detect found
// (detected may be nil: nothing was found, and the file carries a commented
// example instead of a gate). The text always parses with Load: a test checks
// that it round-trips.
func RenderFile(detected *Repo) string {
	var b strings.Builder
	b.WriteString("# heavyq.toml: what \"proven\" means for this repository.\n")
	if detected == nil || len(detected.Gates) == 0 {
		b.WriteString("#\n# Written by `heavyq init`. It found no go.mod, package.json, pytest\n" +
			"# configuration or Cargo.toml, so no gate is defined yet: uncomment the example\n" +
			"# below and put the commands that prove your code in it.\n")
		b.WriteString(fileHeader)
		b.WriteString("# [[gate]]\n# name = \"ready\"\n# run  = [\"make test\"]\n# prio = 3\n")
		return b.String()
	}
	g := detected.Gates[0]
	fmt.Fprintf(&b, "#\n# Written by `heavyq init` from what it found at the repository root: %s.\n", strings.Join(g.Sources, ", "))
	b.WriteString("# Review it, edit it, commit it.\n")
	b.WriteString(fileHeader)
	fmt.Fprintf(&b, "[[gate]]\nname = %s\nrun  = [\n", tomlString(g.Name))
	for _, cmd := range g.Run {
		fmt.Fprintf(&b, "  %s,\n", tomlString(cmd))
	}
	fmt.Fprintf(&b, "]\nprio = %d\n", g.Prio)
	return b.String()
}

// tomlString quotes s as a TOML basic string.
func tomlString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch {
		case r == '"' || r == '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case r < 0x20 || r == 0x7f:
			fmt.Fprintf(&b, `\u%04X`, r)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}
