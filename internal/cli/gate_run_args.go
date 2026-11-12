package cli

import (
	"strconv"
	"strings"

	"github.com/LysCongThanh/heavyq/internal/proof"
	"github.com/LysCongThanh/heavyq/internal/repoconfig"
)

// gateRunArgs is the argument list of `heavyq <prio> ...` for one gate
// command. The command is the exact argv proof.Argv names, so what runs is
// what the ledger is later matched against. fresh leaves out --cache, for the
// re-run that confirms a flaky command.
func gateRunArgs(g repoconfig.Gate, cmd, lane string, fresh bool) []string {
	args := []string{strconv.Itoa(g.Prio), "--gate", g.Name, "--remote", g.Remote, "--strict"}
	if !fresh {
		args = append(args, "--cache")
	}
	if lane != "" {
		args = append(args, "--lane", lane)
	}
	if g.Light {
		args = append(args, "--light")
	}
	if len(g.Needs) > 0 {
		args = append(args, "--needs", strings.Join(g.Needs, ","))
	}
	if len(g.Tools) > 0 { // only placement reads them: not part of the run id, so not of the proof
		args = append(args, "--tools", strings.Join(g.Tools, ","))
	}
	for _, w := range g.With {
		args = append(args, "--with", w)
	}
	if g.NeedGH {
		args = append(args, "--need-gh")
	}
	args = append(args, "--")
	return append(args, proof.Argv(cmd)...)
}
