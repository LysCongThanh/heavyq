package cli

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

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
	remote := fs.String("remote", "", "where the commands run: local | auto | <remote name> | a,b (first that fits) (default: the gate's remote, else [proof] remote, else local)")
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
	cfgPath := absConfigPath(cfg) // before the chdir below: children need it
	// Commands run from the repository root, whatever directory we were
	// started in: that is what a gate command means, and what proof matches.
	if wd, err := os.Getwd(); err == nil {
		defer func() { _ = os.Chdir(wd) }()
	}
	if err := os.Chdir(t.repo.Root); err != nil {
		fmt.Fprintln(stderr, "heavyq:", err)
		return ExitUsage
	}
	before := evaluateGate(cfg, t, stderr)
	if before.Tree == "" {
		fmt.Fprintf(stderr, "heavyq: gate %s cannot be proven: %s\n", t.gate.Name, before.Reason)
		if outputMode(stderr).Terse() {
			fmt.Fprintln(stderr, gateNext(before))
		}
		return 1
	}
	g := t.gate
	var todo []int
	fresh := map[int]bool{} // flaky commands: a re-run must really run, not replay a cached pass
	for i, c := range before.Commands {
		if c.State == proof.Proven {
			fmt.Fprintf(stderr, "heavyq: gate %s [%d/%d] proven: %s\n", g.Name, i+1, len(g.Run), redact.Command(c.Cmd))
		} else {
			todo = append(todo, i)
			fresh[i] = c.Class == proof.ClassFlaky
		}
	}
	if len(todo) > 0 {
		var rc int
		if g, rc = placeGate(cfg, t, *remote, stderr); rc != 0 {
			return rc
		}
	}
	rcs := runGateCommands(cfg, cfgPath, g, *lane, todo, fresh, *parallel, *keepGoing, stdout, stderr)
	for _, i := range todo {
		if rc, ran := rcs[i]; ran && stoppedFromOutside(rc) {
			return rc // stopped from outside: not a failure of the gate
		}
	}
	after := evaluateGate(cfg, t, stderr)
	terse := outputMode(stderr).Terse()
	if terse {
		printGateTable(stdout, after, t.label()) // the closing "next:" line comes last, below
	} else {
		printGateLabeled(stdout, after, t.label())
	}
	if after.Proven {
		return 0
	}
	if terse { // the last line of the output, whichever explanation below is printed
		defer fmt.Fprintln(stderr, gateNext(after))
	}
	if after.Tree != before.Tree {
		fmt.Fprintf(stderr, "heavyq: the work tree changed while the gate ran (%s, now %s); results belong to the earlier code. Run the gate again.\n",
			treeLabel(before), treeLabel(after))
	} else if allPassed(rcs) {
		fmt.Fprintln(stderr, "heavyq: every command exited 0, but the ledger does not prove the gate on this tree (see above)")
	}
	return 1
}

// placeGate returns t's gate with Remote set to where its commands run now:
// resolveProofRemote's answer, tempered by fitProofToTree. rc is non-zero
// (the exit code, after a message) when the gate must not start.
func placeGate(cfg *config.Config, t *gateTarget, remoteFlag string, stderr io.Writer) (repoconfig.Gate, int) {
	g := t.gate
	pl, err := resolveProofRemote(cfg, t.repo, &g, remoteFlag)
	if err != nil {
		fmt.Fprintln(stderr, "heavyq:", err)
		return g, ExitUsage
	}
	if len(g.Needs) > 0 && remoteFlag == "" && !config.TargetMayRunLocally(pl.Remote) {
		// Pool values are resources of this machine; [proof] remote is a default, not a demand.
		fmt.Fprintf(stderr, "heavyq: gate %s needs pool values of this machine: running locally, not on %s (%s)\n", g.Name, pl.Remote, pl.From)
		pl = proofPlace{config.LocalName, pl.From}
	}
	if g.NeedGH && !config.TargetMayRunLocally(pl.Remote) {
		if remoteFlag != "" {
			fmt.Fprintf(stderr, "heavyq: gate %s needs this machine's GitHub login (need_gh), so --remote %s is not possible\n", g.Name, pl.Remote)
			return g, ExitUsage
		}
		fmt.Fprintf(stderr, "heavyq: gate %s needs this machine's GitHub login (need_gh): running locally, not on %s (%s)\n", g.Name, pl.Remote, pl.From)
		pl = proofPlace{config.LocalName, pl.From}
	}
	pl, why := fitProofToTree(t.repo.Root, pl, stderr)
	if why != "" {
		fmt.Fprintln(stderr, "heavyq:", why)
		if outputMode(stderr).Terse() {
			fmt.Fprintln(stderr, "next: heavyq gate run "+g.Name+" --remote local")
		}
		return g, 1
	}
	g.Remote = pl.Remote
	return g, 0
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
// exit codes by index. Sequentially (in this process) it stops at the first
// failure unless keepGoing. In parallel every command is its own child heavyq
// process (see childSet), all started at once; the queue still limits how many
// run.
func runGateCommands(cfg *config.Config, cfgPath string, g repoconfig.Gate, lane string, todo []int, fresh map[int]bool, parallel, keepGoing bool, stdout, stderr io.Writer) map[int]int {
	say := func(i int, what string) {
		fmt.Fprintf(stderr, "heavyq: gate %s [%d/%d] %s: %s\n", g.Name, i+1, len(g.Run), what, redact.Command(g.Run[i]))
	}
	failed := func(i, rc int) {
		if rc != 0 {
			fmt.Fprintf(stderr, "heavyq: gate %s [%d/%d] FAILED (exit %d): %s\n", g.Name, i+1, len(g.Run), rc, redact.Command(g.Run[i]))
		}
	}
	if parallel {
		return runGateInChildren(cfgPath, g, lane, todo, fresh, say, failed, stdout, stderr)
	}
	rcs := make(map[int]int, len(todo))
	for n, i := range todo {
		say(i, "running")
		rc := runCommand(cfg, gateRunArgs(g, g.Run[i], lane, fresh[i]), stderr)
		failed(i, rc)
		rcs[i] = rc
		if stoppedFromOutside(rc) || (rc != 0 && !keepGoing) {
			for _, j := range todo[n+1:] {
				say(j, "not run")
			}
			break
		}
	}
	return rcs
}

// runGateInChildren is the parallel mode: one child heavyq per command, output
// prefixed `[cmd N]` with N the command's number in the gate (as in [N/total]).
func runGateInChildren(cfgPath string, g repoconfig.Gate, lane string, todo []int, fresh map[int]bool,
	say func(int, string), failed func(int, int), stdout, stderr io.Writer) map[int]int {
	rcs := make(map[int]int, len(todo))
	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintln(stderr, "heavyq: cannot find my own executable to start the commands:", err)
		for _, i := range todo {
			rcs[i] = 1
		}
		return rcs
	}
	cs := &childSet{exe: exe, cfg: cfgPath, stdout: stdout, stderr: stderr}
	args := make(map[int][]string, len(todo))
	keys := make([]int, len(todo))
	for n, i := range todo {
		say(i, "running")
		keys[n] = i + 1
		args[i+1] = gateRunArgs(g, g.Run[i], lane, fresh[i])
	}
	byKey := cs.runAll(keys, args)
	for _, i := range todo {
		rcs[i] = byKey[i+1]
		failed(i, rcs[i])
	}
	return rcs
}

// absConfigPath is the config file this process loaded, as an absolute path
// ("" when it ran on defaults), so a child started from another directory
// reads the same file.
func absConfigPath(cfg *config.Config) string {
	if cfg.Path == "" {
		return ""
	}
	if abs, err := filepath.Abs(cfg.Path); err == nil {
		return abs
	}
	return cfg.Path
}

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
	for _, w := range g.With {
		args = append(args, "--with", w)
	}
	if g.NeedGH {
		args = append(args, "--need-gh")
	}
	args = append(args, "--")
	return append(args, proof.Argv(cmd)...)
}
