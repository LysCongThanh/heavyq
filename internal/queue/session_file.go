package queue

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// A session file tells heavyq runs on a lane which agent session they belong
// to, without putting the id into the command line: the Claude Code hook
// writes <queue dir>/sessions/<lane> before the rewritten command runs, and
// `heavyq <prio>` reads it when no --session / $HEAVYQ_SESSION was given.
//
// Two sessions working on the same lane within SessionTTL of each other can
// swap labels. That only mislabels the session in the ledger; proof matches on
// the tree, never on the session.

// SessionTTL is how long a session file stays usable (and unpruned).
const SessionTTL = 10 * time.Minute

const (
	sessionsSub     = "sessions"
	maxSessionBytes = 4 << 10
	// sessionClockSkew tolerates a file stamped slightly in the future.
	sessionClockSkew = time.Minute
)

type sessionFile struct {
	SessionID string    `json:"session_id"`
	At        time.Time `json:"at"`
}

func (d *Dir) sessionsPath() string { return filepath.Join(d.Path, sessionsSub) }

// WriteSession records id as the current session of lane, atomically (temp
// file + rename, mode 0600). The caller validates id.
func (d *Dir) WriteSession(lane, id string, at time.Time) error {
	dir := d.sessionsPath()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("sessions dir: %w", err)
	}
	b, err := json.Marshal(sessionFile{SessionID: id, At: at.UTC()})
	if err != nil {
		return err
	}
	// Lane names never start with ".", so a temp name cannot be a lane's file.
	tmp, err := os.CreateTemp(dir, ".tmp-")
	if err != nil {
		return fmt.Errorf("write session: %w", err)
	}
	name := tmp.Name()
	if _, err = tmp.Write(b); err == nil {
		err = tmp.Chmod(0o600)
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(name, filepath.Join(dir, SanitizeLane(lane)))
	}
	if err != nil {
		_ = os.Remove(name)
		return fmt.Errorf("write session: %w", err)
	}
	return nil
}

// ReadSession returns the session recorded for lane, or "" when there is
// none, it is older than SessionTTL (or stamped in the future), the file is
// not a plain small JSON file, or valid rejects the id.
func (d *Dir) ReadSession(lane string, now time.Time, valid func(string) bool) string {
	path := filepath.Join(d.sessionsPath(), SanitizeLane(lane))
	fi, err := os.Lstat(path)
	if err != nil || !fi.Mode().IsRegular() || fi.Size() > maxSessionBytes {
		return ""
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var f sessionFile
	if json.Unmarshal(b, &f) != nil || !valid(f.SessionID) {
		return ""
	}
	if age := now.Sub(f.At); age >= SessionTTL || age < -sessionClockSkew {
		return ""
	}
	return f.SessionID
}

// pruneSessions removes session files (and temp files left by a crashed
// writer) that have not been touched for SessionTTL.
func (d *Dir) pruneSessions(now time.Time) {
	ents, err := os.ReadDir(d.sessionsPath())
	if err != nil {
		return
	}
	for _, e := range ents {
		if fi, err := e.Info(); err == nil && now.Sub(fi.ModTime()) >= SessionTTL {
			_ = os.Remove(filepath.Join(d.sessionsPath(), e.Name()))
		}
	}
}
