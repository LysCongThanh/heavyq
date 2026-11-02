package cli

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/LysCongThanh/heavyq/internal/history"
	"github.com/LysCongThanh/heavyq/internal/ledgerindex"
	"github.com/LysCongThanh/heavyq/internal/preflight"
	"github.com/LysCongThanh/heavyq/internal/runspan"
	"github.com/LysCongThanh/heavyq/internal/steer"
)

// preflightNudgeLine is the extra line of the SessionStart brief that points a
// long session at `heavyq preflight`.
const preflightNudgeLine = "next: heavyq preflight\n"

// nudgeMarkerTTL is how long the "already said it" marker of a session stays.
const nudgeMarkerTTL = 14 * 24 * time.Hour

// preflightNudge returns the brief line when the lane has items, no run span is
// open and the session looks long (preflight.ShouldNudge), and "" otherwise.
// It says it at most once per session: the first call that returns the line
// leaves a marker file under the git common dir. Any doubt (no session id, an
// unreadable ledger or directory) is silence.
func preflightNudge(common, sessionID string, facts steer.Facts, now time.Time) string {
	if !steer.ValidSessionID(sessionID) || len(facts.Items) == 0 || facts.Lane == "" {
		return ""
	}
	if cur, err := runspan.Current(common, now); err != nil || cur != nil {
		return ""
	}
	recs, _, err := ledgerindex.LaneRuns(history.DefaultPath(), facts.Lane)
	if err != nil || !preflight.ShouldNudge(recs, true, false, now) {
		return ""
	}
	if !claimNudge(common, sessionID, now) {
		return ""
	}
	return preflightNudgeLine
}

// appendNudge adds the nudge line to the injected text, on a line of its own.
func appendNudge(text, nudge string) string {
	if nudge == "" {
		return text
	}
	if !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	return text + nudge
}

// claimNudge creates the session's marker; false when it already exists (the
// nudge was given) or cannot be created.
func claimNudge(common, sessionID string, now time.Time) bool {
	dir := filepath.Join(common, "heavyq", "preflight-nudged")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return false
	}
	f, err := os.OpenFile(filepath.Join(dir, sessionID), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return false
	}
	f.Close()
	pruneNudgeMarkers(dir, now)
	return true
}

// pruneNudgeMarkers drops the markers of sessions long gone (best effort).
func pruneNudgeMarkers(dir string, now time.Time) {
	entries, err := os.ReadDir(dir)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return
	}
	for _, e := range entries {
		if info, err := e.Info(); err == nil && now.Sub(info.ModTime()) > nudgeMarkerTTL {
			_ = os.Remove(filepath.Join(dir, e.Name()))
		}
	}
}
