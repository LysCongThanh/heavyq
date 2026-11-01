package proof

import (
	"strings"

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
	return wanted{hash: history.CmdHash(argv), text: text, textOK: text == redact.Command(strings.Join(argv, " "))}
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

// MatcherFor returns the test that says whether a ledger record is a run of
// the gate command cmd, by the same rule proofs use (cmd_hash when the record
// has one, else the command string). It does not look at tree, repository or
// outcome.
func MatcherFor(cmd string) func(history.Record) bool { return wantCommand(cmd).matches }
