package flowstate

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

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

// The quota mark is the same idea for the fleet: one file for the whole state
// directory that says "a usage limit stopped agents, in these repositories".
// PostToolUse of any session, in any repository, checks it with one failed read
// when there is none, and clears the quota everywhere it was recorded once
// activity is past the grace.

type quotaMark struct {
	At      time.Time `json:"at"` // the latest stop
	Commons []string  `json:"commons"`
}

func quotaMarkPath() string {
	d := marksDir()
	if d == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(d), "flow-quota.json")
}

// MarkQuota records a usage-limit stop at `at` in the repository whose shared
// directory is common.
func MarkQuota(common string, at time.Time) error {
	p := quotaMarkPath()
	if p == "" || common == "" {
		return nil
	}
	m := quotaMark{At: at.UTC()}
	if b, err := os.ReadFile(p); err == nil {
		_ = json.Unmarshal(b, &m)
		if at.After(m.At) {
			m.At = at.UTC()
		}
	}
	known := false
	for _, c := range m.Commons {
		known = known || c == common
	}
	if !known {
		m.Commons = append(m.Commons, common)
	}
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	return os.WriteFile(p, b, 0o600)
}

// TakeQuotaMark removes the quota mark and returns the repositories it named,
// when the latest stop is at least grace old: activity right after a stop is
// agents finishing their last tool calls, not the quota coming back. With no
// mark (the cheap, common answer) or a fresh one, it returns nothing, and a
// fresh mark stays.
func TakeQuotaMark(now time.Time, grace time.Duration) []string {
	p := quotaMarkPath()
	if p == "" {
		return nil
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return nil
	}
	var m quotaMark
	if json.Unmarshal(b, &m) != nil {
		_ = os.Remove(p) // damaged: forget it
		return nil
	}
	if now.Sub(m.At) < grace {
		return nil
	}
	if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return m.Commons
}
