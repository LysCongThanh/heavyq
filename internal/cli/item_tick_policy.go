package cli

import (
	"errors"
	"fmt"

	"github.com/LysCongThanh/heavyq/internal/anchor"
	"github.com/LysCongThanh/heavyq/internal/gitutil"
	"github.com/LysCongThanh/heavyq/internal/repoconfig"
)

// tickRetries is how often the box is retried when the anchor file is edited
// under us (anchor.ErrConcurrent).
const tickRetries = 3

// tickItem ticks the box of anchor item it in the work tree at root. Callers
// hold the lease store's lock (the anchor package serialises nothing itself).
func tickItem(root string, it anchor.Item) error {
	var err error
	for i := 0; i < tickRetries; i++ {
		if err = anchor.SetItemChecked(root, it, true); !errors.Is(err, anchor.ErrConcurrent) {
			break
		}
	}
	return err
}

// tickBox says whether `item done` ticks the box in the anchor file (`[items]
// tick`). When it does not, why says so in a few words: the done is still
// recorded in the events log, which every work tree of the repository shares,
// and `heavyq item sync` ticks the box later, once, where the anchor is committed.
//
// "auto" ticks unless the repository has more than one work tree: lanes in
// parallel work trees ticking different, usually adjacent, lines of one file is
// a merge conflict (#119). When git cannot say, auto does not tick: a deferred
// tick costs a command, a conflicting one costs a merge.
func (c *itemCtx) tickBox() (tick bool, why string) {
	switch c.repo.ItemLimits().Tick {
	case repoconfig.TickAlways:
		return true, ""
	case repoconfig.TickNever:
		return false, `[items] tick = "never"`
	}
	trees, err := gitutil.Worktrees(c.root)
	switch {
	case err != nil:
		return false, "git did not list the work trees"
	case len(trees) > 1:
		return false, fmt.Sprintf("the repository has %d work trees", len(trees))
	}
	return true, ""
}
