package cli

import (
	"fmt"
	"io"
	"os"

	"github.com/LysCongThanh/heavyq/internal/history"
	"github.com/LysCongThanh/heavyq/internal/proof"
	"github.com/LysCongThanh/heavyq/internal/repoconfig"
)

// gateWhyLine is the closing sentence of `heavyq why` when the repository
// around the current directory has a heavyq.toml: whether its main gate is
// proven on the code checked out now. It is "" when there is no heavyq.toml,
// no unambiguous gate (see pickGate), or the file is invalid (said on stderr).
// recs is the ledger, oldest first.
func gateWhyLine(recs []history.Record, stderr io.Writer) string {
	wd, err := os.Getwd()
	if err != nil {
		return ""
	}
	repo, err := repoconfig.Load(wd)
	if err != nil {
		fmt.Fprintln(stderr, "heavyq:", err)
		return ""
	}
	if repo == nil {
		return ""
	}
	g, ok := pickGate(repo, "", io.Discard)
	if !ok {
		return ""
	}
	st := proof.Evaluate(g.Name, g.Run, subjectOf(repo.Root), recs)
	switch {
	case st.Proven:
		return fmt.Sprintf("The %s gate is proven on this tree (%s).", g.Name, treeLabel(st))
	case st.Tree == "":
		return fmt.Sprintf("The %s gate cannot be proven here: %s.", g.Name, st.Reason)
	}
	failed, missing := 0, 0
	for _, c := range st.Commands {
		if c.State == proof.Failed {
			failed++
		} else if c.State != proof.Proven {
			missing++
		}
	}
	return fmt.Sprintf("The %s gate is not proven on this tree (%s): %d failed, %d missing. Prove it with: heavyq gate run %s",
		g.Name, treeLabel(st), failed, missing, g.Name)
}
