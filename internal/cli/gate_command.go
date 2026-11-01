package cli

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/LysCongThanh/heavyq/internal/config"
	"github.com/LysCongThanh/heavyq/internal/gitutil"
	"github.com/LysCongThanh/heavyq/internal/history"
	"github.com/LysCongThanh/heavyq/internal/proof"
	"github.com/LysCongThanh/heavyq/internal/repoconfig"
)

// defaultGate is the gate used when none is named and heavyq.toml has it.
const defaultGate = "ready"

// ledgerWindow bounds how many ledger records a proof check reads. Older runs
// cannot prove anything: a proof must be recent enough to be found, and a
// missing one only costs a (cached) re-run.
const ledgerWindow = 20000

const gateUsage = `usage:
  heavyq [--config FILE] gate status [name] [--json]
  heavyq [--config FILE] gate run [name] [--parallel] [--keep-going] [--lane NAME]

name defaults to "ready", else to the only gate in heavyq.toml.
status exits 0 when every command is proven on this exact code, 1 when not, 2 on a usage error.
`

// gateCommand implements `heavyq gate status|run`.
func gateCommand(cfg *config.Config, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, gateUsage)
		return ExitUsage
	}
	switch args[0] {
	case "status":
		return gateStatusCommand(args[1:], stdout, stderr)
	case "run":
		return gateRunCommand(cfg, args[1:], stdout, stderr)
	}
	fmt.Fprintf(stderr, "heavyq: unknown gate command %q (status, run)\n\n%s", args[0], gateUsage)
	return ExitUsage
}

// parseGateArgs parses flags and at most one gate name, in any order
// (`gate status ready --json` and `gate status --json ready` both work).
func parseGateArgs(fs *flag.FlagSet, args []string, stderr io.Writer) (name string, ok bool) {
	fs.SetOutput(stderr)
	fs.Usage = func() { fmt.Fprint(stderr, gateUsage) }
	var names []string
	for {
		if err := fs.Parse(args); err != nil {
			return "", false
		}
		if fs.NArg() == 0 {
			break
		}
		names = append(names, fs.Arg(0))
		args = fs.Args()[1:]
	}
	if len(names) > 1 || (len(names) == 1 && names[0] == "") {
		fs.Usage()
		return "", false
	}
	if len(names) == 1 {
		name = names[0]
	}
	return name, true
}

// gateTarget is the repository, gate and code a gate command is about.
type gateTarget struct {
	repo *repoconfig.Repo
	gate repoconfig.Gate
}

// loadGate finds heavyq.toml from the current directory and picks the gate.
// The message is printed for the caller; ok false means exit ExitUsage.
func loadGate(name string, stderr io.Writer) (*gateTarget, bool) {
	wd, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(stderr, "heavyq:", err)
		return nil, false
	}
	repo, err := repoconfig.Load(wd)
	if err != nil {
		fmt.Fprintln(stderr, "heavyq:", err)
		return nil, false
	}
	if repo == nil {
		if top, err := gitutil.TopLevel(wd); err != nil {
			fmt.Fprintf(stderr, "heavyq: %s is not inside a git work tree, so there is no %s\n", wd, repoconfig.FileName)
		} else {
			fmt.Fprintf(stderr, "heavyq: no %s in %s (it defines the gates)\n", repoconfig.FileName, top)
		}
		return nil, false
	}
	g, ok := pickGate(repo, name, stderr)
	if !ok {
		return nil, false
	}
	return &gateTarget{repo: repo, gate: g}, true
}

// pickGate resolves name against the file: the named gate, else "ready", else
// the only gate. Anything ambiguous is refused with the list of gates.
func pickGate(repo *repoconfig.Repo, name string, stderr io.Writer) (repoconfig.Gate, bool) {
	var names []string
	for _, g := range repo.Gates {
		names = append(names, g.Name)
	}
	list := strings.Join(names, ", ")
	switch {
	case len(names) == 0:
		fmt.Fprintf(stderr, "heavyq: %s defines no gates\n", repo.Path)
	case name != "":
		if g, ok := repo.Gate(name); ok {
			return g, true
		}
		fmt.Fprintf(stderr, "heavyq: no gate %q in %s (gates: %s)\n", name, repo.Path, list)
	default:
		if g, ok := repo.Gate(defaultGate); ok {
			return g, true
		}
		if len(repo.Gates) == 1 {
			return repo.Gates[0], true
		}
		fmt.Fprintf(stderr, "heavyq: %s has no gate %q; name one of: %s\n", repo.Path, defaultGate, list)
	}
	return repoconfig.Gate{}, false
}

// subjectOf identifies the code in root. A tree that cannot be identified is
// reported with the reason instead of an error: it simply proves nothing.
func subjectOf(root string) proof.Subject {
	co, err := gitutil.Identify(root)
	s := proof.Subject{Repo: co.Repo, Tree: co.Tree, Commit: co.Commit, Dirty: co.Dirty}
	switch {
	case s.Tree != "":
	case err != nil:
		s.Why = err.Error()
	case co.Commit == "":
		s.Why = "the repository has no commits yet"
	case co.Dirty:
		s.Why = "a submodule or nested repository has changes of its own, or there is more untracked content than heavyq can identify"
	}
	return s
}

// evaluateGate is the proof state of t's gate on the code in its repository
// right now, read from the ledger.
func evaluateGate(t *gateTarget, stderr io.Writer) proof.Gate {
	recs, err := history.ReadLast(history.DefaultPath(), ledgerWindow)
	if err != nil {
		// Fail closed: an unreadable ledger might hide a later failure, so
		// nothing is proven from a partial read.
		fmt.Fprintln(stderr, "heavyq: ledger unreadable, so nothing can be proven:", err)
		recs = nil
	}
	return proof.Evaluate(t.gate.Name, t.gate.Run, subjectOf(t.repo.Root), recs)
}
