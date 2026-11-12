package cli

import (
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/LysCongThanh/heavyq/internal/config"
	"github.com/LysCongThanh/heavyq/internal/gitutil"
	"github.com/LysCongThanh/heavyq/internal/history"
	"github.com/LysCongThanh/heavyq/internal/itemstatus"
	"github.com/LysCongThanh/heavyq/internal/redact"
	"github.com/LysCongThanh/heavyq/internal/snapshot"
)

var itemsCache = struct {
	sync.Mutex
	m map[string]cachedLaneItems
}{m: map[string]cachedLaneItems{}}

type cachedLaneItems struct {
	items []snapshot.LaneItems
	at    time.Time
}

// laneItems is what each lane holds or waits on in the work items of the
// repositories lanes ran in lately, plus the repository the caller is in.
// Only repositories where someone ever claimed an item (a lease store exists)
// are looked at, within the same budget as the gate proofs.
func laneItems(cfg *config.Config, recs []history.Record, now time.Time) []snapshot.LaneItems {
	out := []snapshot.LaneItems{}
	var roots []string
	for i, t := range activeTrees(recs, now) {
		if i >= maxGateWorktrees {
			break
		}
		if _, err := os.Stat(t.worktree); err == nil {
			roots = append(roots, t.worktree)
		}
	}
	if wd, err := os.Getwd(); err == nil {
		roots = append(roots, wd)
	}
	start, seen := time.Now(), map[string]bool{}
	for _, root := range roots {
		if time.Since(start) > gateProofBudget {
			break
		}
		common, err := gitutil.CommonDir(root)
		if err != nil || seen[common] || !hasLeaseStore(common) {
			continue
		}
		seen[common] = true
		out = append(out, cachedRepoItems(cfg, root, common, recs, now)...)
	}
	return out
}

func cachedRepoItems(cfg *config.Config, root, common string, recs []history.Record, now time.Time) []snapshot.LaneItems {
	itemsCache.Lock()
	defer itemsCache.Unlock()
	if c, ok := itemsCache.m[common]; ok && time.Since(c.at) < gateProofTTL {
		return c.items
	}
	items := repoLaneItems(cfg, root, recs, now)
	itemsCache.m[common] = cachedLaneItems{items, time.Now()}
	return items
}

// repoLaneItems groups the claimed, proven and blocked items of the repository
// around root by lane, in anchor order.
func repoLaneItems(cfg *config.Config, root string, recs []history.Record, now time.Time) []snapshot.LaneItems {
	set, err := loadItemSet(cfg, root)
	if err != nil || len(set.file.Items) == 0 {
		return nil
	}
	rows, err := set.rows(recs, now, true)
	if err != nil {
		return nil
	}
	var out []snapshot.LaneItems
	at := map[string]int{}
	for _, r := range rows {
		s := r.Status
		if s.Lane == "" || (s.State != itemstatus.Claimed && s.State != itemstatus.Proven && s.State != itemstatus.Blocked) {
			continue
		}
		i, ok := at[s.Lane]
		if !ok {
			i = len(out)
			at[s.Lane] = i
			out = append(out, snapshot.LaneItems{Lane: s.Lane, Repo: filepath.Base(set.root), Items: []snapshot.ItemChip{}})
		}
		chip := snapshot.ItemChip{ID: s.ID, State: s.State, Needs: redact.Command(s.Needs), At: s.At}
		if s.State == itemstatus.Proven {
			chip.Commit = s.Commit
		}
		out[i].Items = append(out[i].Items, chip)
	}
	return out
}
