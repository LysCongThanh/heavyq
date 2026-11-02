package statusline

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"
)

// The status line is the only place the agent's version and its rate limits
// are visible (its stdin JSON), so it leaves a tiny record per session that the
// ledger can join on: <git common dir>/heavyq/session-meta/<session id>.json.
//
// TODO(#102): internal/sessionmeta owns this record. When it lands, delete this
// writer and call it instead; the file name and fields are deliberately the
// obvious ones so the swap is a rename.

// SessionMeta is the record.
type SessionMeta struct {
	SessionID    string    `json:"session_id"`
	Model        string    `json:"model,omitempty"`
	AgentVersion string    `json:"agent_version,omitempty"`
	FiveHourPct  *int      `json:"five_hour_pct,omitempty"`
	SevenDayPct  *int      `json:"seven_day_pct,omitempty"`
	Updated      time.Time `json:"updated"`
}

// metaRefresh: an unchanged record is rewritten this often, so "updated" says
// the session is alive.
const metaRefresh = 5 * time.Minute

// safeSessionID keeps a session id usable as a file name: 1 to 128 of
// [A-Za-z0-9_-] (no regexp: its compile would run at every process start).
func safeSessionID(id string) bool {
	if id == "" || len(id) > 128 {
		return false
	}
	for _, r := range id {
		if r > 0x7e || !(r == '_' || r == '-' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z') {
			return false
		}
	}
	return true
}

// MetaDir is where the records of the repository with this common dir live.
func MetaDir(commonDir string) string { return filepath.Join(commonDir, "heavyq", "session-meta") }

// WriteSessionMeta records what the status line saw, only when it changed (or
// is old): one small read in the common case. Nothing is written without a
// usable session id.
func WriteSessionMeta(commonDir string, in Input, now time.Time) error {
	if !safeSessionID(in.SessionID) || (in.Model == "" && in.Version == "") {
		return nil
	}
	rec := SessionMeta{SessionID: in.SessionID, Model: in.Model, AgentVersion: in.Version, Updated: now.UTC()}
	if in.FiveHour >= 0 {
		rec.FiveHourPct = &in.FiveHour
	}
	if in.SevenDay >= 0 {
		rec.SevenDayPct = &in.SevenDay
	}
	dir := MetaDir(commonDir)
	path := filepath.Join(dir, in.SessionID+".json")
	existed := false
	if b, err := os.ReadFile(path); err == nil {
		existed = true
		var old SessionMeta
		if json.Unmarshal(b, &old) == nil && sameMeta(old, rec) && now.Sub(old.Updated) < metaRefresh {
			return nil
		}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	b, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".meta-*.tmp")
	if err != nil {
		return err
	}
	_, werr := tmp.Write(append(b, '\n'))
	if err := errors.Join(werr, tmp.Close()); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	if !existed {
		pruneMeta(dir, now) // a new session is the moment to forget the old ones
	}
	return nil
}

// metaKeep is how long a session's record is kept after its last update.
const metaKeep = 14 * 24 * time.Hour

func pruneMeta(dir string, now time.Time) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if info, err := e.Info(); err == nil && now.Sub(info.ModTime()) > metaKeep {
			_ = os.Remove(filepath.Join(dir, e.Name()))
		}
	}
}

func sameMeta(a, b SessionMeta) bool {
	eq := func(x, y *int) bool { return (x == nil) == (y == nil) && (x == nil || *x == *y) }
	return a.SessionID == b.SessionID && a.Model == b.Model && a.AgentVersion == b.AgentVersion &&
		eq(a.FiveHourPct, b.FiveHourPct) && eq(a.SevenDayPct, b.SevenDayPct)
}
