package proof

import (
	"github.com/LysCongThanh/heavyq/internal/history"
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

// commandStatus collects the conclusive runs of cmd on subject and decides.
func commandStatus(cmd string, subject Subject, recs []history.Record) Command {
	out := Command{Cmd: cmd, State: Missing}
	if subject.Tree == "" || subject.Repo == "" {
		return out
	}
	m := newMatcher(cmd, subject)
	var runs []history.Record // oldest first
	near := ""                // why a run of the same command on this tree does not count
	for _, r := range recs {
		if r.Tree != subject.Tree || r.Repo != subject.Repo || r.Subdir != "" || r.Result != "" {
			continue // another code, another place, or never ran to its end
		}
		if !m.matches(r) {
			if near == "" {
				near = m.whyNot(r)
			}
			continue // another command (or the same one in another environment)
		}
		runs = append(runs, r)
	}
	if len(runs) == 0 {
		switch {
		case near != "":
			out.Reason = near
		case !m.w.textOK:
			out.Reason = longCommandReason
		}
		return out
	}
	return decide(out, runs)
}
