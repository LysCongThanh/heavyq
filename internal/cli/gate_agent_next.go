package cli

import (
	"fmt"
	"io"

	"github.com/LysCongThanh/heavyq/internal/proof"
)

// gateNext is the "next:" line of a gate that is not proven: the one thing to do.
func gateNext(g proof.Gate) string {
	if g.Tree == "" { // nothing can be proven on code heavyq cannot identify
		return "next: make the tree identifiable (see above), then run: heavyq gate status " + g.Gate
	}
	return "next: heavyq gate run " + g.Gate
}

// printGateFor renders g for a person or, when terse, for an agent: the same
// table, but the closing sentence becomes the single "next:" line. label is
// the gate's name in the heading ("ready (detected)" for a detected gate).
func printGateFor(w io.Writer, g proof.Gate, label string, terse bool) {
	if !terse {
		printGateLabeled(w, g, label)
		return
	}
	printGateTable(w, g, label)
	if !g.Proven {
		fmt.Fprintln(w, gateNext(g))
	}
}
