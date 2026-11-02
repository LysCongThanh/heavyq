package stance

import (
	"sort"
	"strings"

	"github.com/LysCongThanh/heavyq/internal/gitutil"
)

// Churn is the evidence behind enter.churn_reverts: work that is being undone
// or redone without a proof in between.
type Churn struct {
	Reverts  int      // revert commits
	Rewrites int      // commits that redo exactly the file set of an earlier one
	Files    []string // files of those commits, sorted, at most maxChurnFiles
}

// Count is the number of churn events, what the threshold is compared with.
func (c Churn) Count() int { return c.Reverts + c.Rewrites }

const maxChurnFiles = 3

// ChurnReverts counts churn in commits, which are the commits made since the
// lane's last proof, newest first (gitutil.RecentCommits). Two kinds of event:
//
//   - a revert: a commit whose subject starts with "Revert " (what git revert
//     writes), the work of an earlier commit taken back;
//   - a rewrite: a non-revert commit that changes exactly the same set of
//     files as an earlier commit of the window, the same code redone.
//
// A proof between commits ends the window (the caller cuts it), so ordinary
// edit, run, commit loops never count. Time is not evidence here: only what
// happened to the files.
func ChurnReverts(commits []gitutil.CommitFiles) Churn {
	var c Churn
	seen := map[string]bool{}
	touched := map[string]bool{}
	for i := len(commits) - 1; i >= 0; i-- { // oldest first
		cm := commits[i]
		if len(cm.Files) == 0 {
			continue
		}
		files := append([]string(nil), cm.Files...)
		sort.Strings(files)
		key := strings.Join(files, "\x00")
		switch {
		case strings.HasPrefix(cm.Subject, "Revert "):
			c.Reverts++
		case seen[key]:
			c.Rewrites++
		default:
			seen[key] = true
			continue
		}
		seen[key] = true
		for _, f := range files {
			touched[f] = true
		}
	}
	for f := range touched {
		c.Files = append(c.Files, f)
	}
	sort.Strings(c.Files)
	if len(c.Files) > maxChurnFiles {
		c.Files = c.Files[:maxChurnFiles]
	}
	return c
}
