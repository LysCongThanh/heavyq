package cli

import (
	"os"
	"path/filepath"

	"github.com/LysCongThanh/heavyq/internal/gitutil"
	"github.com/LysCongThanh/heavyq/internal/lease"
)

// renewLaneLeases keeps the claims of lane alive: any heavyq run in a lane
// shows the agent is working, so the run extends that lane's item leases by a
// full TTL when it ends. It never fails a run: every error is dropped, and a
// repository that has no lease store (nobody ever claimed an item) is left
// untouched, not even created.
func renewLaneLeases(lane string) {
	defer func() { _ = recover() }()
	wd, err := os.Getwd()
	if err != nil {
		return
	}
	common, err := gitutil.CommonDir(wd)
	if err != nil {
		return
	}
	if !hasLeaseStore(common) {
		return
	}
	if store, err := lease.Open(common); err == nil {
		_, _ = store.RenewLane(lane)
	}
}

// hasLeaseStore reports whether anyone ever used items in the repository with
// this git common dir. Read paths check it first so they never create the
// store as a side effect.
func hasLeaseStore(common string) bool {
	fi, err := os.Stat(filepath.Join(common, "heavyq", "leases"))
	return err == nil && fi.IsDir()
}
