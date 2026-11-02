package mentor

import "fmt"

// Prefix opens every note, so the agent can tell tool data from user text and
// a reader of a transcript can tell where it came from.
const Prefix = "[heavyq · tool data] "

// RenderMessage returns the note for s: `[heavyq · tool data] <fact>.` at the
// 2nd failure, with ` Next: <one action>.` from the 3rd. One line, built from
// structured fields only (exit code, count, tree, the ledger's command): the
// output of the failing command is never pasted.
func RenderMessage(s Signal) string {
	tree := s.Tree
	if len(tree) > 12 {
		tree = tree[:12]
	}
	if s.Level < LevelDirective {
		return fmt.Sprintf("%s`%s` exited %d twice in a row on the same tree (%s).", Prefix, s.Cmd, s.Exit, tree)
	}
	return fmt.Sprintf("%s`%s` exited %d %d times in a row on the same tree (%s). "+
		"Next: change the code or its inputs before rerunning, or report BLOCKED with needs:.",
		Prefix, s.Cmd, s.Exit, s.Streak, tree)
}
