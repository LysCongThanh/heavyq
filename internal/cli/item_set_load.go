package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/LysCongThanh/heavyq/internal/anchor"
	"github.com/LysCongThanh/heavyq/internal/config"
	"github.com/LysCongThanh/heavyq/internal/gitutil"
	"github.com/LysCongThanh/heavyq/internal/history"
	"github.com/LysCongThanh/heavyq/internal/itemstatus"
	"github.com/LysCongThanh/heavyq/internal/lease"
	"github.com/LysCongThanh/heavyq/internal/proof"
	"github.com/LysCongThanh/heavyq/internal/repoconfig"
)

// eventsWindow is how much of the tail of events.jsonl status derivation
// reads. A block older than that (about 10 000 events) is forgotten.
const eventsWindow = 4 << 20

// itemSet is the work items of one repository as seen from one work tree: the
// anchor file and extra sources of that work tree, the repository's leases and
// events (shared by all its work trees), and where proofs are evaluated.
type itemSet struct {
	cfg   *config.Config
	root  string           // work tree the anchor was read from
	repo  *repoconfig.Repo // heavyq.toml (or the detected default gate); nil when there is none
	file  *anchor.File
	store *lease.Store
	// subjects caches the identity of each work tree looked at, by root and scope.
	subjects map[string]proof.Subject
	// worktrees is `git worktree list`, read when a lane's tree is needed.
	worktrees []string
}

// loadItemSet reads the items of the repository around dir.
func loadItemSet(cfg *config.Config, dir string) (*itemSet, error) {
	root, err := gitutil.TopLevel(dir)
	if err != nil {
		return nil, fmt.Errorf("%s is not inside a git work tree", dir)
	}
	repo, err := repoconfig.LoadOrDetect(root)
	if err != nil {
		return nil, err
	}
	var acfg repoconfig.Anchor
	if repo != nil {
		acfg = repo.Anchor
	}
	file, err := anchor.Load(root, acfg)
	if err != nil {
		return nil, err
	}
	common, err := gitutil.CommonDir(root)
	if err != nil {
		return nil, err
	}
	store, err := lease.Open(common)
	if err != nil {
		return nil, err
	}
	return &itemSet{cfg: cfg, root: root, repo: repo, file: file, store: store,
		subjects: map[string]proof.Subject{}}, nil
}

// subject is the identity of the code in work tree root, cached.
func (s *itemSet) subject(root string) proof.Subject { return s.scopedSubject(root, nil) }

// proofSubject is the identity of the code in work tree root that proof p is judged on: the files named by the
// `paths` of the gate p stands for, else the whole tree.
func (s *itemSet) proofSubject(root string, p anchor.Proof) proof.Subject {
	var scope []string
	if p.Kind == anchor.ProofGate {
		if g, ok := proofGate(s, p.Value); ok {
			scope = g.Paths
		}
	}
	return s.scopedSubject(root, scope)
}

// scopedSubject is subject for the code a gate's `paths` (scope) name, cached by work tree and scope.
func (s *itemSet) scopedSubject(root string, scope []string) proof.Subject {
	key := root + "\x00" + strings.Join(scope, "\x00")
	if sub, ok := s.subjects[key]; ok {
		return sub
	}
	sub := subjectOfWith(s.cfg, root, scope, nil)
	s.subjects[key] = sub
	return sub
}

// laneTree is the work tree a lane works in: the work tree whose directory
// name is the lane, else the one its latest ledger run was in, else the work
// tree the items were read from.
func (s *itemSet) laneTree(lane string, recs []history.Record) string {
	if s.worktrees == nil {
		s.worktrees, _ = gitutil.Worktrees(s.root)
		if s.worktrees == nil {
			s.worktrees = []string{}
		}
	}
	for _, wt := range s.worktrees {
		if laneOf(filepath.Base(wt)) == lane {
			return wt
		}
	}
	for i := len(recs) - 1; i >= 0; i-- {
		r := recs[i]
		if r.Lane != lane || r.Worktree == "" {
			continue
		}
		for _, wt := range s.worktrees {
			if wt == r.Worktree {
				if _, err := os.Stat(wt); err == nil {
					return wt
				}
			}
		}
	}
	return s.root
}

// itemRow is an item with its derived status.
type itemRow struct {
	Item   anchor.Item
	Status itemstatus.Status
}

// rows derives the status of every item. A claimed item is judged on the work
// tree of the lane that holds it, every other on s.root. With heldOnly, items
// nobody holds, blocks or ticked are not evaluated against the ledger (they
// read as open): the Tower only wants to know who holds what.
func (s *itemSet) rows(recs []history.Record, now time.Time, heldOnly bool) ([]itemRow, error) {
	leases, err := s.store.List()
	if err != nil {
		return nil, err
	}
	byItem := make(map[string]*lease.Lease, len(leases))
	for i := range leases {
		byItem[leases[i].Item] = &leases[i]
	}
	events, err := s.store.Events(eventsWindow)
	if err != nil {
		return nil, err
	}
	marks := itemstatus.Marks(events)
	ev := newEvidenceCache(s, recs)
	out := make([]itemRow, 0, len(s.file.Items))
	for _, it := range s.file.Items {
		l, m := byItem[it.ID], marks[it.ID]
		live := l != nil && !l.Stale && !now.After(l.Expires)
		root := s.root
		if live {
			root = s.laneTree(l.Lane, recs)
		}
		proofEv := itemstatus.Evidence{Declared: it.Proof.Kind != ""}
		if !heldOnly || live || m.Blocked || it.Checked || it.Needs != "" {
			proofEv = ev.of(it, root)
		}
		st := itemstatus.Derive(itemstatus.Input{Item: it, Lease: l, Mark: m, Proof: proofEv, Now: now})
		out = append(out, itemRow{Item: it, Status: st})
	}
	return out, nil
}
