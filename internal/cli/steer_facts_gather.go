package cli

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/LysCongThanh/heavyq/internal/anchor"
	"github.com/LysCongThanh/heavyq/internal/config"
	"github.com/LysCongThanh/heavyq/internal/gitutil"
	"github.com/LysCongThanh/heavyq/internal/history"
	"github.com/LysCongThanh/heavyq/internal/itemstatus"
	"github.com/LysCongThanh/heavyq/internal/metrics"
	"github.com/LysCongThanh/heavyq/internal/procinfo"
	"github.com/LysCongThanh/heavyq/internal/proof"
	"github.com/LysCongThanh/heavyq/internal/queue"
	"github.com/LysCongThanh/heavyq/internal/redact"
	"github.com/LysCongThanh/heavyq/internal/repoconfig"
	"github.com/LysCongThanh/heavyq/internal/steer"
)

// gatherSteerFacts reads what the brief and the stop gate need about the
// repository around dir, for the agent working in lane. It only reads: no gate
// runs, and a repository that never used items is not touched (no lease store
// is created for it). Every proof comes from the ledger index.
func gatherSteerFacts(cfg *config.Config, dir, lane string) (steer.Facts, error) {
	root, err := gitutil.TopLevel(dir)
	if err != nil {
		return steer.Facts{}, fmt.Errorf("%s is not inside a git work tree", dir)
	}
	repo, err := repoconfig.LoadOrDetect(root)
	if err != nil {
		return steer.Facts{}, err
	}
	f := steer.Facts{Lane: lane, Items: []steer.ItemFact{}, Machine: machineLine()}
	subj := subjectOf(cfg, root)
	f.Tree = subj.Tree
	now := time.Now()
	recs := recentRuns()

	var set *itemSet
	var acfg repoconfig.Anchor // zero without a heavyq.toml: the default anchor file
	if repo != nil {
		acfg = repo.Anchor
	}
	if file, err := anchor.Load(root, acfg); err == nil && (file.Goal != "" || len(file.Items) > 0) {
		if set, err = loadItemSet(cfg, root); err != nil {
			return steer.Facts{}, err
		}
		f.Goal = set.file.Goal
	}
	if repo != nil {
		if g, ok := pickGate(repo, "", io.Discard); ok {
			f.Gate = gateFact(g, subjectOfWith(cfg, root, g.Paths, nil), recs)
		}
	}
	if set == nil {
		return f, nil
	}
	rows, err := set.rows(recs, now, false)
	if err != nil {
		return steer.Facts{}, err
	}
	memo := map[string][]history.Record{}
	for _, r := range rows {
		f.Items = append(f.Items, itemFact(set, r, recs, memo))
	}
	return f, nil
}

// recentRuns is the lane-indexed ledger window used to find a lane's work tree
// and older passing runs. An unreadable ledger proves nothing and says so
// nowhere: hooks stay quiet.
func recentRuns() []history.Record { return ledgerOrNone(io.Discard) }

// gateFact is the main gate's state on subj, and whether it passed on an
// earlier tree of this repository.
func gateFact(g repoconfig.Gate, subj proof.Subject, recs []history.Record) *steer.GateFact {
	pg := proof.Evaluate(g.Name, g.Run, subj, treeRecords(subj))
	ev := itemstatus.EvidenceFor(pg, subj, recs)
	return &steer.GateFact{Name: g.Name, Proven: pg.Proven, Tree: subj.Tree, Failing: failedCmds(pg), Older: ev.Older != nil}
}

// treeRecords are the conclusive runs on subj's tree; an unreadable ledger is
// empty (fail closed: nothing is proven from it).
func treeRecords(subj proof.Subject) []history.Record {
	recs, err := proofRecords(subj)
	if err != nil {
		return nil
	}
	return recs
}

// itemFact turns an item row into the facts the steering code uses. A claimed
// item's failing commands are judged on the tree of the lane that holds it,
// like its status; an item nobody works on, on the tree the items were read from.
func itemFact(set *itemSet, r itemRow, recs []history.Record, memo map[string][]history.Record) steer.ItemFact {
	st := r.Status
	it := steer.ItemFact{ID: st.ID, Text: r.Item.Text, State: st.State, Lane: st.Lane, Needs: st.Needs,
		Commit: st.Commit, Done: st.Done, NoProof: r.Item.Proof.Kind == "", Reason: redact.Command(st.Reason)}
	if !st.Expires.IsZero() {
		it.Until = st.Expires.Local().Format("15:04")
	}
	switch st.State {
	case itemstatus.Open, itemstatus.Claimed, itemstatus.ClaimedDoneUnproven, itemstatus.Stale:
	default:
		return it
	}
	name, cmds, why := proofCommands(set, r.Item.Proof)
	if r.Item.Proof.Kind == "" || why != "" {
		return it
	}
	root := set.root
	if st.Lane != "" {
		root = set.laneTree(st.Lane, recs)
	}
	subj := set.proofSubject(root, r.Item.Proof)
	tr, ok := memo[subj.Tree]
	if !ok {
		tr = treeRecords(subj)
		memo[subj.Tree] = tr
	}
	it.Failing = failedCmds(proof.Evaluate(name, cmds, subj, tr))
	return it
}

// failedCmds are the commands of g whose latest conclusive run failed, with
// the exit code, as structured fields. Command lines are redacted.
func failedCmds(g proof.Gate) []steer.FailedCmd {
	var out []steer.FailedCmd
	for _, c := range g.Commands {
		if c.State != proof.Failed {
			continue
		}
		fc := steer.FailedCmd{Cmd: redact.Command(c.Cmd), Class: string(c.Class)}
		if c.Exit != nil {
			fc.Exit = *c.Exit
		}
		out = append(out, fc)
	}
	return out
}

// machineLine is the one machine-pressure line of the brief: load, free RAM
// and the queue. "" when nothing can be measured.
func machineLine() string {
	var parts []string
	if m := metrics.Host(procinfo.Default.Root); m.Known {
		parts = append(parts, fmt.Sprintf("load %.1f on %d cores", m.Load1, m.Cores), fmt.Sprintf("%.1f GB free", float64(m.FreeMB)/1024))
	}
	if _, err := os.Stat(QueueDir()); err == nil { // never create the queue dir for a brief
		if q, err := queue.Open(QueueDir()); err == nil {
			if s, err := q.List(); err == nil {
				parts = append(parts, fmt.Sprintf("queue %d running, %d waiting", len(s.Running), len(s.Waiting)))
			}
		}
	}
	return strings.Join(parts, ", ")
}
