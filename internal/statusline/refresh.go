package statusline

import (
	"os"
	"path/filepath"
	"time"

	"github.com/LysCongThanh/heavyq/internal/flowview"
)

// StaleAfter: a flow state older than this is refreshed in the background.
const StaleAfter = 60 * time.Second

// refreshEvery throttles the spawn: the status line runs every few hundred
// milliseconds, a refresh must not be started on each of them.
const refreshEvery = 15 * time.Second

// NeedsRefresh says whether a state that exists has gone stale. A repository
// with no state at all is left alone: heavyq has never been used there, and
// the status line must not start touching every repository an agent opens.
func NeedsRefresh(st *flowview.State, now time.Time) bool {
	return st != nil && now.Sub(st.Updated) > StaleAfter
}

// ClaimRefresh is true for at most one caller per refreshEvery: it touches a
// marker file and checks its age. Two callers racing may both win; a refresh
// is idempotent, so that costs one extra process, not a wrong answer.
func ClaimRefresh(commonDir string, now time.Time) bool {
	dir := filepath.Join(commonDir, "heavyq", "flow")
	marker := filepath.Join(dir, ".refresh-requested")
	if fi, err := os.Stat(marker); err == nil && now.Sub(fi.ModTime()) < refreshEvery {
		return false
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return false
	}
	f, err := os.OpenFile(marker, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return false
	}
	_ = f.Close()
	return os.Chtimes(marker, now, now) == nil
}
