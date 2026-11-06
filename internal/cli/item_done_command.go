package cli

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/LysCongThanh/heavyq/internal/anchor"
	"github.com/LysCongThanh/heavyq/internal/history"
	"github.com/LysCongThanh/heavyq/internal/itemstatus"
	"github.com/LysCongThanh/heavyq/internal/lease"
	"github.com/LysCongThanh/heavyq/internal/proof"
	"github.com/LysCongThanh/heavyq/internal/repoconfig"
)

// doneJSON is the `item done --json` document.
type doneJSON struct {
	Item   string `json:"item"`
	State  string `json:"state"` // always "done"
	Commit string `json:"commit"`
	Ticked bool   `json:"ticked"` // the box in the anchor file was ticked (false for plan-file items)
	Source string `json:"source"`
}

// tickRetries is how often the box is retried when the anchor file is edited
// under us (anchor.ErrConcurrent).
const tickRetries = 3

// itemDone implements `heavyq item done <id> [--run [--remote WHERE]] [--json]`. It finishes an
// item only on proof: the item's proof must pass on the code in this work tree
// (--run runs it through the queue first). Then, under the lease store's lock,
// it ticks the box in the anchor file (plan-file items are never rewritten:
// they are marked done in the events log only) and releases the lease.
func itemDone(c *itemCtx, args []string) int {
	fs := flag.NewFlagSet("item done", flag.ContinueOnError)
	runIt := fs.Bool("run", false, "run the proof through the queue first")
	asJSON := fs.Bool("json", false, "machine-readable output")
	remote := fs.String("remote", "", "with --run: where the proof runs: local | auto | <remote name> | a,b (first that fits) (default: [proof] remote, else local)")
	pos, ok := parseItemArgs(fs, args, c.stderr)
	if !ok || len(pos) != 1 {
		fmt.Fprint(c.stderr, itemUsage)
		return ExitUsage
	}
	id := pos[0]
	rows, err := c.rows(ledgerOrNone(c.stderr), time.Now(), true)
	if err != nil {
		fmt.Fprintln(c.stderr, "heavyq:", err)
		return 1
	}
	row, ok := findRow(rows, id)
	if !ok {
		return c.unknownItem(id)
	}
	it, st := row.Item, row.Status
	if st.State == itemstatus.Claimed && st.Lane != c.lane {
		return c.refuse(fmt.Sprintf("item %s is held by lane %s until %s", id, st.Lane, st.Expires.Local().Format("15:04")), "heavyq item claim --next")
	}
	if it.Proof.Kind == "" {
		return c.refuse(fmt.Sprintf("item %s declares no proof, so heavyq cannot prove it: add `proof: <cmd>` or `proof: gate:<name>` to its line in %s (or tick the box by hand)", id, it.Source), "")
	}
	ev := newEvidenceCache(c.itemSet, ledgerOrNone(c.stderr)).of(it, c.root)
	if *remote != "" && !*runIt {
		fmt.Fprintln(c.stderr, "heavyq: --remote only applies with --run")
		return ExitUsage
	}
	if !ev.Proven && *runIt {
		if rc, started := c.runProof(it, *remote); !started || (rc != 0 && stoppedFromOutside(rc)) {
			return rc
		}
		c.subjects = map[string]proof.Subject{} // the tree may have changed while the proof ran
		ev = newEvidenceCache(c.itemSet, ledgerOrNone(c.stderr)).of(it, c.root)
	}
	if !ev.Proven {
		return c.refuseUnproven(it, ev, *runIt)
	}
	return c.finishDone(it, ev, *asJSON)
}

// refuseUnproven says why the item cannot be finished and how to prove it.
func (c *itemCtx) refuseUnproven(it anchor.Item, ev itemstatus.Evidence, ran bool) int {
	why := ev.Reason
	if why == "" {
		why = "its proof has not passed on this exact code"
	}
	if ran {
		why = "its proof was run and does not pass: " + why
	}
	fmt.Fprintf(c.stderr, "heavyq: item %s is not proven on this code: %s\n", it.ID, why)
	next := "heavyq item done " + it.ID + " --run"
	if it.Proof.Kind == anchor.ProofGate && !ran {
		next = "heavyq gate run " + it.Proof.Value
	}
	fmt.Fprintln(c.stderr, "next: "+next)
	return 1
}

// finishDone ticks the box (anchor items), records the done event and releases
// the lease, all under the lease store's lock.
func (c *itemCtx) finishDone(it anchor.Item, ev itemstatus.Evidence, asJSON bool) int {
	tick := func() error {
		if it.ReadOnly {
			return nil
		}
		var err error
		for i := 0; i < tickRetries; i++ {
			if err = anchor.SetItemChecked(c.root, it, true); !errors.Is(err, anchor.ErrConcurrent) {
				break
			}
		}
		return err
	}
	err := c.store.Finish(it.ID, c.ownerFor(it.ID), lease.Event{Op: lease.OpDone, Commit: ev.Commit}, tick)
	var held *lease.HeldError
	switch {
	case errors.As(err, &held):
		return c.refuse(held.Error(), "heavyq item claim --next")
	case err != nil:
		fmt.Fprintln(c.stderr, "heavyq: item not finished:", err)
		return 1
	}
	if asJSON {
		enc := json.NewEncoder(c.stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(doneJSON{Item: it.ID, State: "done", Commit: ev.Commit, Ticked: !it.ReadOnly, Source: it.Source}); err != nil {
			fmt.Fprintln(c.stderr, "heavyq:", err)
			return 1
		}
		return 0
	}
	if it.ReadOnly {
		fmt.Fprintf(c.stdout, "done %s: proven at %s; recorded in heavyq's events log (%s is never rewritten)\n", it.ID, ev.Commit, it.Source)
	} else {
		fmt.Fprintf(c.stdout, "done %s: proven at %s; ticked [x] in %s (commit it)\n", it.ID, ev.Commit, it.Source)
	}
	if c.terse {
		fmt.Fprintln(c.stdout, "next: heavyq item claim --next")
	}
	return 0
}

// runProof runs the item's proof through the queue, tagged with the item, from
// the work tree root (what proof matching requires). It returns the exit code
// of the run: for a gate, 0 only when the whole gate is proven. started is
// false when nothing ran (a usage error, or a refusal such as a named remote
// asked to prove uncommitted code); rc is then the exit code to return.
//
// Where it runs: remoteFlag (--remote), else the gate's own remote, else
// [proof] remote of heavyq.toml, else of the user config, else local.
func (c *itemCtx) runProof(it anchor.Item, remoteFlag string) (rc int, started bool) {
	if wd, err := os.Getwd(); err == nil {
		defer func() { _ = os.Chdir(wd) }()
	}
	if err := os.Chdir(c.root); err != nil {
		fmt.Fprintln(c.stderr, "heavyq:", err)
		return ExitUsage, false
	}
	if tag := strings.ReplaceAll(it.ID, "/", ":"); history.ValidTag(tag) { // ids of plan-file items are longer or have "/"
		prev, had := os.LookupEnv("HEAVYQ_ITEM")
		_ = os.Setenv("HEAVYQ_ITEM", tag)
		defer func() {
			if had {
				_ = os.Setenv("HEAVYQ_ITEM", prev)
			} else {
				_ = os.Unsetenv("HEAVYQ_ITEM")
			}
		}()
	}
	if it.Proof.Kind == anchor.ProofGate {
		args := []string{it.Proof.Value} // the gate run resolves [proof] remote itself
		if remoteFlag != "" {
			args = append([]string{"--remote", remoteFlag}, args...)
		}
		rc = gateRunCommand(c.cfg, args, c.stdout, c.stderr)
		return rc, rc != ExitUsage
	}
	pl, err := resolveProofRemote(c.cfg, c.repo, nil, remoteFlag)
	if err != nil {
		fmt.Fprintln(c.stderr, "heavyq:", err)
		return ExitUsage, false
	}
	pl, why := fitProofToTree(c.root, pl, c.stderr)
	if why != "" {
		return c.refuse(why, "heavyq item done "+it.ID+" --run --remote local"), false
	}
	args := []string{fmt.Sprint(repoconfig.DefaultPrio), "--cache", "--strict", "--remote", pl.Remote, "--"}
	// The proof command's own exit code comes back, so 2 is not "usage" here.
	return runCommand(c.cfg, append(args, proof.Argv(it.Proof.Value)...), c.stderr), true
}
