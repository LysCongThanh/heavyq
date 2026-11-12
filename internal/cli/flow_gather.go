package cli

import (
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/LysCongThanh/heavyq/internal/anchor"
	"github.com/LysCongThanh/heavyq/internal/config"
	"github.com/LysCongThanh/heavyq/internal/flow"
	"github.com/LysCongThanh/heavyq/internal/flowstate"
	"github.com/LysCongThanh/heavyq/internal/gitutil"
	"github.com/LysCongThanh/heavyq/internal/history"
	"github.com/LysCongThanh/heavyq/internal/lease"
	"github.com/LysCongThanh/heavyq/internal/proof"
	"github.com/LysCongThanh/heavyq/internal/redact"
	"github.com/LysCongThanh/heavyq/internal/repoconfig"
	"github.com/LysCongThanh/heavyq/internal/sessionmeta"
)

// flowLookback is how far behind the window the facts are read: runs and events
// that began before the window can still end inside it.
const flowLookback = time.Hour

// maxGoalChars keeps the anchor goal short enough for a status line.
const maxGoalChars = 200

// gatherFlow reads everything internal/flow derives from, for the repository
// around dir: this repository's runs from the ledger index, the lease events,
// the items as itemstatus derives them, the hook events and the session meta
// record. It only reads. It returns the git common dir the state belongs to.
// Sources that do not exist (no anchor file, no lease store) drop their
// states; they are not errors.
func gatherFlow(cfg *config.Config, dir string, now time.Time) (flow.Input, string, error) {
	root, err := gitutil.TopLevel(dir)
	if err != nil {
		return flow.Input{}, "", err
	}
	common, err := gitutil.CommonDir(dir)
	if err != nil {
		return flow.Input{}, "", err
	}
	co, err := gitutil.Locate(dir)
	if err != nil {
		return flow.Input{}, "", err
	}
	in := flow.Input{Now: now, Repo: repoRootOf(common, root)}

	recent := ledgerOrNone(io.Discard) // 7 days, all repositories: also what item proofs are judged on
	from := now.Add(-flow.DefaultWindow - flowLookback)
	for _, r := range recent {
		if r.Repo == co.Repo && r.Repo != "" && !r.End.Before(from) {
			in.Records = append(in.Records, r)
		}
	}
	if hasLeaseStore(common) {
		if store, err := lease.Open(common); err == nil {
			if evs, err := store.Events(eventsWindow); err == nil {
				in.Leases = evs
			}
		}
	}
	in.Items, in.Goal = flowItems(cfg, root, recent, now)
	if evs, err := flowstate.ReadEvents(common, from); err == nil {
		in.Hooks = evs
	}
	if m := sessionmeta.Latest(common); m != nil {
		in.Session = sessionInfo(m)
	}
	in.GoneLanes = goneLanes(in)
	gatherFlowExtras(&in, common, root, from)
	return in, common, nil
}

// repoRootOf is the main work tree of the repository: the parent of its .git,
// else the work tree dir was found in (a bare or unusual layout).
func repoRootOf(common, root string) string {
	if filepath.Base(common) == ".git" {
		return filepath.Dir(common)
	}
	return root
}

// flowItems are the repository's items with their derived status and the
// commands that count as their proof, and the anchor goal. A repository
// without an anchor goal or items has none, and is not touched.
func flowItems(cfg *config.Config, root string, recent []history.Record, now time.Time) ([]flow.ItemFact, string) {
	repo, err := repoconfig.LoadOrDetect(root)
	if err != nil {
		return nil, ""
	}
	var acfg repoconfig.Anchor
	if repo != nil {
		acfg = repo.Anchor
	}
	file, err := anchor.Load(root, acfg)
	if err != nil || (file.Goal == "" && len(file.Items) == 0) {
		return nil, ""
	}
	set, err := loadItemSet(cfg, root)
	if err != nil {
		return nil, ""
	}
	rows, err := set.rows(recent, now, false)
	if err != nil {
		return nil, ""
	}
	facts := make([]flow.ItemFact, 0, len(rows))
	for _, r := range rows {
		st := r.Status
		st.Needs, st.Reason = redact.Command(st.Needs), redact.Command(st.Reason)
		f := flow.ItemFact{Status: st}
		if name, cmds, why := proofCommands(set, r.Item.Proof); r.Item.Proof.Kind != "" && why == "" {
			if r.Item.Proof.Kind == anchor.ProofGate { // the ledger tags a gate's runs with its name: a command proof has no gate
				f.Gate = name
			}
			for _, c := range cmds {
				f.ProofCmds = append(f.ProofCmds, redact.Command(c))
				if f.Gate != "" {
					f.ProofHashes = append(f.ProofHashes, history.CmdHash(proof.Argv(c)))
				}
			}
		}
		facts = append(facts, f)
	}
	return facts, oneLine(redact.Command(set.file.Goal), maxGoalChars)
}

// sessionInfo is the snapshot's "session" block from a meta record.
func sessionInfo(m *sessionmeta.Meta) *flow.SessionInfo {
	si := &flow.SessionInfo{Model: m.Model, AgentVersion: m.Version}
	si.Updated = m.Updated
	if m.FiveHourPct != nil || m.SevenDayPct != nil || m.FiveHourResetsAt != nil || m.SevenDayResetsAt != nil {
		si.RateLimits = &flow.RateLimits{FiveHourPct: m.FiveHourPct, SevenDayPct: m.SevenDayPct,
			FiveHourResetsAt: m.FiveHourResetsAt, SevenDayResetsAt: m.SevenDayResetsAt}
	}
	return si
}

// agentModelOf is the agent and model a ledger record of this session is
// tagged with, from the session meta record of the repository around dir. All
// empty when the session has no record (a plain terminal) or dir is no
// repository.
func agentModelOf(dir, session string) (agent, model string) {
	if dir == "" || session == "" {
		return "", ""
	}
	common, err := gitutil.CommonDir(dir)
	if err != nil {
		return "", ""
	}
	m, err := sessionmeta.Read(common, session)
	if err != nil || m == nil {
		return "", ""
	}
	return strings.TrimSpace(m.Agent()), m.Model
}
