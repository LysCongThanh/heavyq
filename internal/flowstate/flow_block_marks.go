package flowstate

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/LysCongThanh/heavyq/internal/history"
)

// A block mark is a one-file flag, per session, that says "this session's lane
// is waiting on a person". It exists for one reason: PostToolUse fires after
// every tool call and must clear a block without resolving the repository (a git
// call) or reading a log. With the flag, the common case (nothing blocked) is a
// failed unlink of one file named by the session id.
//
// The truth stays in the events log (internal/flow derives blocks from it); a
// stale flag only costs one extra "cleared" event. Marks live in heavyq's state
// directory beside the ledger, not in the repository, because that is the one
// place reachable from a session id alone.

// blockMark is the content of a mark: where the block was recorded.
type blockMark struct {
	Common string `json:"common"`
	Lane   string `json:"lane"`
}

func marksDir() string {
	h := history.DefaultPath()
	if h == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(h), "flow-blocked")
}

func markPath(session string) string {
	d := marksDir()
	if d == "" || !history.ValidTag(session) {
		return ""
	}
	return filepath.Join(d, session)
}

// MarkBlocked flags session as waiting; lane and common say where the event
// was written. Without a usable state directory it does nothing (the ledger-run
// path still clears the block).
func MarkBlocked(session, common, lane string) error {
	p := markPath(session)
	if p == "" {
		return nil
	}
	b, err := json.Marshal(blockMark{Common: common, Lane: lane})
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	return os.WriteFile(p, b, 0o600)
}

// TakeBlockMark removes the flag of session and returns what it held; ok is
// false when there was none, which is the cheap, common answer.
func TakeBlockMark(session string) (common, lane string, ok bool) {
	p := markPath(session)
	if p == "" {
		return "", "", false
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return "", "", false
	}
	if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", "", false
	}
	var m blockMark
	if json.Unmarshal(b, &m) != nil || m.Common == "" || m.Lane == "" {
		return "", "", false
	}
	return m.Common, m.Lane, true
}
