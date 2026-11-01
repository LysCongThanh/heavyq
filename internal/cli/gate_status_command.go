package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/LysCongThanh/heavyq/internal/proof"
	"github.com/LysCongThanh/heavyq/internal/redact"
)

// gateStatusCommand implements `heavyq gate status [name] [--json]`: is this
// exact code proven? Exit 0 yes, 1 no, 2 usage error, or neither a heavyq.toml
// nor a gate detected from the repository's files.
func gateStatusCommand(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("gate status", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "machine-readable output (schema in docs/gates.md)")
	name, ok := parseGateArgs(fs, args, stderr)
	if !ok {
		return ExitUsage
	}
	t, ok := loadGate(name, stderr)
	if !ok {
		return ExitUsage
	}
	g := evaluateGate(t, stderr)
	if *asJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(statusJSON{Gate: redactedGate(g), Detected: t.gate.Detected}); err != nil {
			fmt.Fprintln(stderr, "heavyq:", err)
			return 1
		}
	} else {
		printGateLabeled(stdout, g, t.label())
	}
	if g.Proven {
		return 0
	}
	return 1
}

// statusJSON is the `gate status --json` document: the proof state, plus
// "detected": true when the gate is the default heavyq detected because the
// repository has no heavyq.toml. The key is absent for a gate from a file.
type statusJSON struct {
	proof.Gate
	Detected bool `json:"detected,omitempty"`
}

// redactedGate hides likely secrets in the command lines of g. Gate commands
// come from a committed file, but the same rule applies to everything that
// prints command lines. (The ledger matching uses the raw string.)
func redactedGate(g proof.Gate) proof.Gate {
	out := g
	out.Commands = make([]proof.Command, len(g.Commands))
	for i, c := range g.Commands {
		c.Cmd = redact.Command(c.Cmd)
		out.Commands[i] = c
	}
	return out
}

// printGate renders g as a table, one line per command.
func printGate(w io.Writer, g proof.Gate) { printGateLabeled(w, g, g.Gate) }

// printGateLabeled is printGate with label as the gate's name in the heading
// ("ready (detected)" for a detected gate). Commands to type still use the
// plain name.
func printGateLabeled(w io.Writer, g proof.Gate, label string) {
	g = redactedGate(g)
	verdict := "NOT PROVEN"
	if g.Proven {
		verdict = "PROVEN"
	}
	fmt.Fprintf(w, "gate %s: %s on %s\n", label, verdict, treeLabel(g))
	if g.Reason != "" {
		fmt.Fprintf(w, "  %s\n", g.Reason)
	}
	fmt.Fprintf(w, "  %-8s %-19s  %-14s %7s  %s\n", "STATE", "WHEN", "LANE", "RUN", "COMMAND")
	counts := map[proof.State]int{}
	for _, c := range g.Commands {
		counts[c.State]++
		when, lane, run := "-", "-", "-"
		if !c.At.IsZero() {
			when = c.At.Local().Format("2006-01-02 15:04:05")
			lane = clip(orDefault(c.Lane, "-"), 14)
			run = fmt.Sprintf("%.1fs", c.RunSecs)
			if c.Cache == "hit" || c.Cache == "coalesced" {
				run = c.Cache
			}
		}
		line := fmt.Sprintf("  %-8s %-19s  %-14s %7s  %s", c.State, when, lane, run, c.Cmd)
		if c.State == proof.Failed && c.Exit != nil {
			line += fmt.Sprintf("  (exit %d)", *c.Exit)
		} else if c.Reason != "" {
			line += "  (" + c.Reason + ")"
		}
		fmt.Fprintln(w, line)
	}
	if !g.Proven {
		fmt.Fprintf(w, "  %d failed, %d missing. Prove it with: heavyq gate run %s\n",
			counts[proof.Failed], counts[proof.Missing], g.Gate)
	}
}

// clip shortens s to at most n bytes (on a character boundary), marking the cut.
func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return strings.ToValidUTF8(s[:n-1], "") + "~"
}

// treeLabel names the code a gate was checked on: the short tree id, and
// "(dirty)" when it includes uncommitted edits.
func treeLabel(g proof.Gate) string {
	if g.Tree == "" {
		return "an unidentified tree"
	}
	// A dirty id is "<HEAD tree>+<digest of the edits>": show both ends, so two
	// different edits of the same commit do not look alike.
	head, digest, edited := strings.Cut(g.Tree, "+")
	label := "tree " + head[:min(12, len(head))]
	if edited {
		label += "+" + digest[:min(8, len(digest))]
	}
	if g.Dirty {
		label += " (uncommitted edits)"
	}
	return strings.TrimSpace(label)
}
