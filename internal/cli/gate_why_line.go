package cli

import (
	"fmt"
	"io"
	"os"

	"github.com/LysCongThanh/heavyq/internal/config"
	"github.com/LysCongThanh/heavyq/internal/history"
	"github.com/LysCongThanh/heavyq/internal/proof"
	"github.com/LysCongThanh/heavyq/internal/repoconfig"
)

// gateWhyLine is the closing sentence of `heavyq why` when the repository
// around the current directory has a heavyq.toml, or a default gate detected
// at its root: whether its main gate is proven on the code checked out now.
// It is "" when there is neither, no unambiguous gate (see pickGate), or the
// file is invalid (said on stderr).
// recs is the ledger, oldest first.
func gateWhyLine(cfg *config.Config, recs []history.Record, stderr io.Writer) string {
	wd, err := os.Getwd()
	if err != nil {
		return ""
	}
	repo, err := repoconfig.LoadOrDetect(wd)
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
	st := proof.Evaluate(g.Name, g.Run, subjectOf(cfg, repo.Root), recs)
	label := gateLabel(g)
	switch {
	case st.Proven:
		return fmt.Sprintf("The %s gate is proven on this tree (%s).", label, treeLabel(st))
	case st.Tree == "":
		return fmt.Sprintf("The %s gate cannot be proven here: %s.", label, st.Reason)
	}
	return fmt.Sprintf("The %s gate is not proven on this tree (%s): %s. Prove it with: heavyq gate run %s",
		label, treeLabel(st), countGate(st), g.Name)
}
