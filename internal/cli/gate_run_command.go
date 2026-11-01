package cli

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/LysCongThanh/heavyq/internal/config"
	"github.com/LysCongThanh/heavyq/internal/proof"
	"github.com/LysCongThanh/heavyq/internal/redact"
	"github.com/LysCongThanh/heavyq/internal/repoconfig"
)

// gateRunCommand implements `heavyq gate run [name]`: run every command of
// the gate that is not yet proven on this exact code, through the queue
// exactly like
//
//	heavyq <prio> --cache --gate <name> --remote <r> -- sh -c '<command>'
//
// Exit 0 only when, afterwards, the ledger proves the whole gate on the code
// that is checked out now. Not the command's own exit code: 1 means "not
// proven" (as `gate status`), and a stop by a signal or an operator cancel is
// passed through (128+n, 78).
func gateRunCommand(cfg *config.Config, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("gate run", flag.ContinueOnError)
	parallel := fs.Bool("parallel", false, "start every unproven command at once (the queue still limits how many run); implies --keep-going")
	keepGoing := fs.Bool("keep-going", false, "run the remaining commands after one fails")
	lane := fs.String("lane", "", "lane name (default: $HEAVYQ_LANE or the repository directory name)")
	name, ok := parseGateArgs(fs, args, stderr)
	if !ok {
		return ExitUsage
	}
	t, ok := loadGate(name, stderr)
	if !ok {
		return ExitUsage
	}
	if t.gate.Detected {
		fmt.Fprintf(stderr, "heavyq: no %s: running the default gate detected from %s (`heavyq init` writes it down)\n",
			repoconfig.FileName, strings.Join(t.gate.Sources, ", "))
	}
	// Commands run from the repository root, whatever directory we were
	// started in: that is what a gate command means, and what proof matches.
	if wd, err := os.Getwd(); err == nil {
		defer func() { _ = os.Chdir(wd) }()
	}
	if err := os.Chdir(t.repo.Root); err != nil {
		fmt.Fprintln(stderr, "heavyq:", err)
		return ExitUsage
	}
	before := evaluateGate(t, stderr)
	if before.Tree == "" {
		fmt.Fprintf(stderr, "heavyq: gate %s cannot be proven: %s\n", t.gate.Name, before.Reason)
		return 1
	}
	g := t.gate
	var todo []int
	for i, c := range before.Commands {
		if c.State == proof.Proven {
			fmt.Fprintf(stderr, "heavyq: gate %s [%d/%d] proven: %s\n", g.Name, i+1, len(g.Run), redact.Command(c.Cmd))
		} else {
			todo = append(todo, i)
		}
	}
	rcs := runGateCommands(cfg, g, *lane, todo, *parallel, *keepGoing, stderr)
	for _, i := range todo {
		if rc, ran := rcs[i]; ran && stoppedFromOutside(rc) {
			return rc // stopped from outside: not a failure of the gate
		}
	}
	after := evaluateGate(t, stderr)
	printGateLabeled(stdout, after, t.label())
	if after.Proven {
		return 0
	}
	if after.Tree != before.Tree {
		fmt.Fprintf(stderr, "heavyq: the work tree changed while the gate ran (%s, now %s); results belong to the earlier code. Run the gate again.\n",
			treeLabel(before), treeLabel(after))
	} else if allPassed(rcs) {
		fmt.Fprintln(stderr, "heavyq: every command exited 0, but the ledger does not prove the gate on this tree (see above)")
	}
	return 1
}

// stoppedFromOutside reports whether rc says the run was ended by an operator
// cancel or by SIGHUP/SIGINT/SIGTERM and friends (128+n), not by the command.
func stoppedFromOutside(rc int) bool { return rc == ExitCancelled || (rc > 128 && rc < 160) }

// allPassed reports whether every command that ran exited 0.
func allPassed(rcs map[int]int) bool {
	for _, rc := range rcs {
		if rc != 0 {
			return false
		}
	}
	return true
}

// runGateCommands runs the commands at the indexes in todo and returns their
// exit codes by index. Sequentially it stops at the first failure unless
// keepGoing; in parallel all of them run.
func runGateCommands(cfg *config.Config, g repoconfig.Gate, lane string, todo []int, parallel, keepGoing bool, stderr io.Writer) map[int]int {
	rcs := make(map[int]int, len(todo))
	one := func(i int) int {
		fmt.Fprintf(stderr, "heavyq: gate %s [%d/%d] running: %s\n", g.Name, i+1, len(g.Run), redact.Command(g.Run[i]))
		rc := runCommand(cfg, gateRunArgs(g, g.Run[i], lane), stderr)
		if rc != 0 {
			fmt.Fprintf(stderr, "heavyq: gate %s [%d/%d] FAILED (exit %d): %s\n", g.Name, i+1, len(g.Run), rc, redact.Command(g.Run[i]))
		}
		return rc
	}
	if parallel {
		var mu sync.Mutex
		var wg sync.WaitGroup
		for _, i := range todo {
			wg.Add(1)
			go func() {
				defer wg.Done()
				rc := one(i)
				mu.Lock()
				rcs[i] = rc
				mu.Unlock()
			}()
		}
		wg.Wait()
		return rcs
	}
	for n, i := range todo {
		rc := one(i)
		rcs[i] = rc
		if stoppedFromOutside(rc) || (rc != 0 && !keepGoing) {
			for _, j := range todo[n+1:] {
				fmt.Fprintf(stderr, "heavyq: gate %s [%d/%d] not run: %s\n", g.Name, j+1, len(g.Run), redact.Command(g.Run[j]))
			}
			break
		}
	}
	return rcs
}

// gateRunArgs is the argument list of `heavyq <prio> ...` for one gate
// command. The command is the exact argv proof.Argv names, so what runs is
// what the ledger is later matched against.
func gateRunArgs(g repoconfig.Gate, cmd, lane string) []string {
	args := []string{strconv.Itoa(g.Prio), "--cache", "--gate", g.Name, "--remote", g.Remote}
	if lane != "" {
		args = append(args, "--lane", lane)
	}
	if g.Light {
		args = append(args, "--light")
	}
	args = append(args, "--")
	return append(args, proof.Argv(cmd)...)
}
