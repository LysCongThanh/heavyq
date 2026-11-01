package cli

import (
	"fmt"
	"io"

	"github.com/LysCongThanh/heavyq/internal/agentmode"
)

// helpCommand implements `heavyq help [topic]`: the usage, or a topic. The
// agents card is a few hundred bytes an agent can afford to read on every
// session start.
func helpCommand(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stdout, usage)
		return 0
	}
	if len(args) == 1 && args[0] == "agents" {
		fmt.Fprint(stdout, agentmode.Card)
		return 0
	}
	fmt.Fprintf(stderr, "heavyq: no help topic %q (topics: agents)\n", args[0])
	return ExitUsage
}
