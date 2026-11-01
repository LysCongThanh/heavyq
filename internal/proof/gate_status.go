package proof

import (
	"strings"

	"github.com/LysCongThanh/heavyq/internal/history"
	"github.com/LysCongThanh/heavyq/internal/queue"
	"github.com/LysCongThanh/heavyq/internal/redact"
)

// Argv is the argv heavyq runs for the gate command cmd: a shell, so quoting,
// pipes and && work as in a shell. `heavyq gate run` and the matching below
// both derive the run from this one function.
func Argv(cmd string) []string { return []string{"sh", "-c", cmd} }

// Evaluate reports, for each command of a gate, whether recs (the ledger,
// oldest first) prove it on subject. It is pure: no I/O, no clock.
func Evaluate(name string, commands []string, subject Subject, recs []history.Record) Gate {
	g := Gate{Gate: name, Tree: subject.Tree, Commit: subject.Commit, Dirty: subject.Dirty,
		Commands: make([]Command, 0, len(commands))}
	if subject.Tree == "" {
		g.Reason = "tree unknown, so nothing can be proven: " + orUnknown(subject.Why)
	}
	g.Proven = len(commands) > 0
	for _, cmd := range commands {
		c := commandStatus(cmd, subject, recs)
		if c.State == Missing && c.Reason == "" && subject.Tree == "" {
			c.Reason = "tree unknown"
		}
		if c.State != Proven {
			g.Proven = false
		}
		g.Commands = append(g.Commands, c)
	}
	return g
}

func orUnknown(why string) string {
	if why == "" {
		return "the work tree could not be identified"
	}
	return why
}

// commandStatus finds the latest conclusive record for cmd on subject.
func commandStatus(cmd string, subject Subject, recs []history.Record) Command {
	out := Command{Cmd: cmd, State: Missing}
	if subject.Tree == "" || subject.Repo == "" {
		return out
	}
	want, ok := ledgerForm(cmd)
	if !ok {
		out.Reason = "longer than the ledger keeps, so a run cannot be matched exactly"
		return out
	}
	for i := len(recs) - 1; i >= 0; i-- {
		r := recs[i]
		if r.Tree != subject.Tree || r.Repo != subject.Repo || r.Cmd != want || r.Subdir != "" || r.Result != "" {
			continue // another code, another command, another place, or never ran to its end
		}
		out.At, out.Lane, out.RunSecs, out.Cache, out.Machine = r.End, r.Lane, r.RunSecs, r.Cache, r.Machine
		exit := r.Exit
		out.Exit = &exit
		out.State = Failed
		if r.Exit == 0 {
			out.State = Proven
		}
		return out
	}
	return out
}

// ledgerForm is how the ledger spells the run of cmd. ok is false when the
// ledger would have shortened it: two different long commands could then look
// alike, so such a command is never matched (fail closed).
func ledgerForm(cmd string) (string, bool) {
	argv := Argv(cmd)
	rendered := queue.TruncateCmd(argv)
	return rendered, rendered == redact.Command(strings.Join(argv, " "))
}
