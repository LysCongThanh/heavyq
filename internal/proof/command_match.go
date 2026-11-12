package proof

import (
	"github.com/LysCongThanh/heavyq/internal/history"
	"github.com/LysCongThanh/heavyq/internal/queue"
	"github.com/LysCongThanh/heavyq/internal/redact"
)

// longCommandReason is why a command that no run proves cannot be proven by
// the string rule: the ledger lines written before cmd_hash cut it short.
const longCommandReason = "longer than older ledger lines keep; run it again to record its hash"

// wanted is how the ledger spells one gate command.
type wanted struct {
	hash string // history.CmdHash of the argv heavyq runs: what a record with cmd_hash is matched on
	// text is the redacted, cut-off form older records carry in cmd; textOK is
	// false when that form would have been cut or altered, so two different
	// long commands could look alike (such a command is never matched by text:
	// fail closed).
	text   string
	textOK bool
}

func wantCommand(cmd string) wanted {
	argv := Argv(cmd)
	text := queue.TruncateCmd(argv)
	return wanted{hash: history.CmdHash(argv), text: text, textOK: text == redact.Argv(argv)}
}

// matches reports whether r is a run of the wanted command. A record with a
// cmd_hash is matched on it alone, however long or secret the command was;
// one without (written by an older heavyq) falls back to the string.
func (w wanted) matches(r history.Record) bool {
	if r.CmdHash != "" {
		return r.CmdHash == w.hash
	}
	return w.textOK && r.Cmd == w.text
}

// Reasons a command is missing although a run of it exists on the tree.
const (
	reasonOtherRunID = "it ran on this tree with a different environment (cache.env) or base ref: run it again"
	reasonNoRunID    = "an older run has no run id and cache.env is set, so it may have run with other variables: run it again"
)

// matcher decides whether a ledger record is a run of one gate command on a
// Subject. The rules, in order:
//
//  1. record and Subject both have a run_id: they match iff the ids are equal.
//     The id covers tree, argv, subdir, allowlisted env values and base ref.
//  2. otherwise (an older record without run_id, or a Subject that cannot
//     compute one) the record is matched by cmd_hash, else by the redacted
//     string, but only when the Subject says the environment cannot make two
//     runs differ (EnvFree). Failing closed: when it might, there is no match.
type matcher struct {
	w       wanted
	id      string // run_id of the gate command on the Subject; "" = unknown
	envFree bool
}

func newMatcher(cmd string, s Subject) matcher {
	m := matcher{w: wantCommand(cmd), envFree: s.EnvFree}
	if s.RunID != nil {
		m.id = s.RunID(Argv(cmd))
	}
	return m
}

func (m matcher) matches(r history.Record) bool {
	if r.RunID != "" && m.id != "" {
		return r.RunID == m.id
	}
	return m.envFree && m.w.matches(r)
}

// whyNot explains a record that runs the same command (by cmd_hash or string)
// but is not a match: "" when it is not even that.
func (m matcher) whyNot(r history.Record) string {
	switch {
	case !m.w.matches(r) || m.matches(r):
		return ""
	case r.RunID != "":
		return reasonOtherRunID
	}
	return reasonNoRunID
}

// MatcherFor returns the test that says whether a ledger record is a run of
// the gate command cmd on s, by the same rules proofs use (see matcher). It
// does not look at tree, repository or outcome.
func MatcherFor(cmd string, s Subject) func(history.Record) bool { return newMatcher(cmd, s).matches }
