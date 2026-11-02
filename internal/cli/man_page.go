package cli

import (
	"fmt"
	"io"
	"strings"
)

// roff escapes text for a man page: backslashes and hyphens (so options can be
// searched and pasted), and a leading "." or "'" that would start a request.
func roff(s string) string {
	s = strings.ReplaceAll(s, `\`, `\e`)
	s = strings.ReplaceAll(s, "-", `\-`)
	if strings.HasPrefix(s, ".") || strings.HasPrefix(s, "'") {
		s = `\&` + s
	}
	return s
}

// roffLines escapes every line of a paragraph.
func roffLines(s string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = roff(l)
	}
	return strings.Join(lines, "\n")
}

// WriteManPage writes the heavyq(1) manual page in roff, generated from the
// command table, the exit codes and the environment variables. date is shown
// in the footer ("" leaves it out), so a release can pin it for reproducible
// archives. See tools/mangen and the goreleaser hook that calls it.
func WriteManPage(w io.Writer, version, date string) {
	var b strings.Builder
	fmt.Fprintf(&b, ".TH HEAVYQ 1 %q %q %q\n", date, "heavyq "+version, "User Commands")
	b.WriteString(".SH NAME\nheavyq \\- a priority, resource\\-aware queue for heavy commands\n")
	b.WriteString(".SH SYNOPSIS\n.nf\n")
	for _, c := range commandTable() {
		for _, line := range c.Synopsis {
			b.WriteString(roff(line) + "\n")
		}
	}
	b.WriteString(".fi\n.SH DESCRIPTION\n")
	b.WriteString(roffLines(`heavyq queues heavy shell commands by priority, admits them when RAM and load allow, can offload them to
other machines over SSH by exact commit, caches results by git tree, and says whether this exact code
passed its gate. There is no daemon: the queue is a directory of ticket files and flock(2) slot files.`) + "\n")
	b.WriteString(".PP\n" + roffLines("Lower priority numbers run first; equal priorities are first in, first out.") + "\n")
	b.WriteString(".SH COMMANDS\n")
	for _, c := range commandTable() {
		writeManCommand(&b, c)
	}
	b.WriteString(".SH ENVIRONMENT\n")
	for _, e := range manEnvironment {
		fmt.Fprintf(&b, ".TP\n\\fB%s\\fR\n%s\n", roff(e[0]), roff(e[1]))
	}
	b.WriteString(".SH EXIT STATUS\n")
	for _, e := range manExitCodes {
		fmt.Fprintf(&b, ".TP\n\\fB%s\\fR\n%s\n", roff(e[0]), roff(e[1]))
	}
	b.WriteString(".SH FILES\n")
	for _, e := range manFiles {
		fmt.Fprintf(&b, ".TP\n\\fI%s\\fR\n%s\n", roff(e[0]), roff(e[1]))
	}
	b.WriteString(".SH SEE ALSO\nFull documentation: " + roff(docsURL) + "\n")
	_, _ = io.WriteString(w, b.String())
}

// writeManCommand writes one command as a subsection.
func writeManCommand(b *strings.Builder, c commandSpec) {
	name := c.Name
	if name == "run" {
		name = "<prio 1\\-9>"
	} else {
		name = roff(name)
	}
	fmt.Fprintf(b, ".SS %s\n%s\n", name, roff(c.Summary))
	b.WriteString(".PP\n.nf\n")
	for _, line := range c.Synopsis {
		b.WriteString(roff(line) + "\n")
	}
	b.WriteString(".fi\n")
	for _, s := range c.Subs {
		fmt.Fprintf(b, ".TP\n\\fB%s\\fR\n%s\n", roff(strings.TrimSpace(s.Name+" "+s.Args)), roff(s.Summary))
	}
	for _, f := range c.Flags {
		fmt.Fprintf(b, ".TP\n\\fB%s\\fR\n%s\n", roff(f.display()), roff(f.Help))
	}
	for _, s := range c.Subs {
		for _, f := range s.Flags {
			fmt.Fprintf(b, ".TP\n\\fB%s\\fR\n%s\n", roff(f.display()), roff("("+s.Name+") "+f.Help))
		}
	}
	if c.Notes != "" {
		b.WriteString(".PP\n.nf\n" + roffLines(c.Notes) + "\n.fi\n")
	}
}

var manEnvironment = [][2]string{
	{"HEAVYQ_CONFIG", "Config file (default $XDG_CONFIG_HOME/heavyq/config.toml); --config wins."},
	{"HEAVYQ_DIR", "Queue directory (default $XDG_RUNTIME_DIR/heavyq, else /tmp/heavyq-<uid>)."},
	{"HEAVYQ_LANE", "Default --lane."},
	{"HEAVYQ_REMOTE", "Default --remote."},
	{"HEAVYQ_AUTO", "0 means never auto-place a run on another machine."},
	{"HEAVYQ_SESSION, HEAVYQ_ITEM", "Default --session and --item, recorded in the history."},
	{"HEAVYQ_OUTPUT, HEAVYQ_AGENT", "Output mode: agent, human or json; 0 or 1 forces agent mode off or on."},
	{"HEAVYQ_HISTORY", "One history file (default: monthly files in $XDG_STATE_HOME/heavyq)."},
}

var manExitCodes = [][2]string{
	{"the command's own", "The command ran; this is its exit code (128+n if killed by signal n)."},
	{"2", "Usage or setup error; nothing ran."},
	{"75", "--max-wait elapsed before the ticket's turn."},
	{"76", "The run can only go to a remote and its remotes were unreachable for too long."},
	{"77", "Remote preparation failed."},
	{"78", "Cancelled by an operator (heavyq cancel or the dashboard)."},
	{"255", "The connection to the remote was lost mid-run."},
}

var manFiles = [][2]string{
	{"~/.config/heavyq/config.toml", "User configuration: slots, RAM budgets, remotes, hook rules, cache."},
	{"heavyq.toml", "At the repository root: the gates that say what \"proven\" means, and pools."},
	{".heavyq/anchor.md", "The checklist of work items for heavyq item."},
}
