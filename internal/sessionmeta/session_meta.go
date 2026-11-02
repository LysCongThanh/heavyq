// Package sessionmeta keeps one small record per coding-agent session: the
// model, the agent version and the rate-limit headroom, at
// <git common dir>/heavyq/sessions/<session>.meta.json.
//
// The only complete source is the Claude Code status line, whose stdin JSON
// carries model.id, version and rate_limits (no hook input carries the agent
// version, and SessionStart.model is optional). So whatever runs on every
// status-line refresh calls RecordStatusline, and the ledger joins its
// agent/model fields on the session id (see Meta.Agent). PostModelSwitch
// updates the model and keeps a bounded list of change points
// (RecordModelSwitch).
//
// Writes are atomic (temp file + rename) and skipped when nothing changed, so
// a 300 ms status-line cadence costs a small read, not a write.
package sessionmeta

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/LysCongThanh/heavyq/internal/history"
)

// schema is written to every file as "schema".
const schema = 1

// maxSwitches bounds the change points one session keeps.
const maxSwitches = 16

// rewriteEvery is how often an unchanged record is rewritten anyway, so
// Updated says "the status line was alive at this moment".
const rewriteEvery = time.Minute

// agentName is the agent every meta record describes today.
const agentName = "claude-code"

// Switch is one model change point (PostModelSwitch).
type Switch struct {
	At   time.Time `json:"at"`
	From string    `json:"from,omitempty"`
	To   string    `json:"to"`
}

// Meta is the session record. Percentages are 0..100 and nil when the
// status line carried no such window (non-subscription users, first turn).
type Meta struct {
	Schema      int       `json:"schema"`
	Session     string    `json:"session"`
	Model       string    `json:"model,omitempty"`
	Version     string    `json:"version,omitempty"` // agent version, e.g. 2.1.90
	FiveHourPct *float64  `json:"five_hour_pct,omitempty"`
	SevenDayPct *float64  `json:"seven_day_pct,omitempty"`
	Updated     time.Time `json:"updated"`
	Switches    []Switch  `json:"switches,omitempty"`
}

// Agent is the ledger's agent field: "claude-code/2.1.90", or "claude-code"
// when the version is unknown.
func (m Meta) Agent() string {
	if m.Version == "" {
		return agentName
	}
	return agentName + "/" + m.Version
}

// Dir is where the records of the repository with this git common dir live.
func Dir(common string) string { return filepath.Join(common, "heavyq", "sessions") }

// path is the file of session; ok is false for an id that is not a safe tag
// (session ids come from outside and become a file name).
func path(common, session string) (string, bool) {
	if common == "" || !history.ValidTag(session) {
		return "", false
	}
	return filepath.Join(Dir(common), session+".meta.json"), true
}

// Read returns the record of session. A missing file is (nil, nil).
func Read(common, session string) (*Meta, error) {
	p, ok := path(common, session)
	if !ok {
		return nil, nil
	}
	b, err := os.ReadFile(p)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("session meta: %w", err)
	}
	var m Meta
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("session meta %s: %w", session, err)
	}
	return &m, nil
}

// Latest is the most recently updated record of the repository, nil when
// there is none: the "session" block of the flow state. Corrupt files are
// skipped.
func Latest(common string) *Meta {
	entries, err := os.ReadDir(Dir(common))
	if err != nil {
		return nil
	}
	var best *Meta
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		b, err := os.ReadFile(filepath.Join(Dir(common), e.Name()))
		if err != nil {
			continue
		}
		var m Meta
		if json.Unmarshal(b, &m) != nil || m.Session == "" {
			continue
		}
		if best == nil || m.Updated.After(best.Updated) {
			best = &m
		}
	}
	return best
}

// write stores m atomically.
func write(common string, m Meta) error {
	p, ok := path(common, m.Session)
	if !ok {
		return fmt.Errorf("session meta: %q is not a valid session id", m.Session)
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return fmt.Errorf("session meta: %w", err)
	}
	m.Schema = schema
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(p), ".meta-*")
	if err != nil {
		return fmt.Errorf("session meta: %w", err)
	}
	_, werr := tmp.Write(append(b, '\n'))
	cerr := tmp.Close()
	if err := errors.Join(werr, cerr); err != nil {
		os.Remove(tmp.Name())
		return fmt.Errorf("session meta: %w", err)
	}
	if err := os.Rename(tmp.Name(), p); err != nil {
		os.Remove(tmp.Name())
		return fmt.Errorf("session meta: %w", err)
	}
	return nil
}

// keep is how long a session's record is kept after its last update.
const keep = 14 * 24 * time.Hour

// prune removes the records nobody has touched for two weeks.
func prune(common string, now time.Time) {
	entries, err := os.ReadDir(Dir(common))
	if err != nil {
		return
	}
	for _, e := range entries {
		if info, err := e.Info(); err == nil && !e.IsDir() && now.Sub(info.ModTime()) > keep {
			_ = os.Remove(filepath.Join(Dir(common), e.Name()))
		}
	}
}

// RecordModelSwitch notes that the session moved to model `to` at `at`: the
// record's model changes and the change point is kept (the newest
// maxSwitches). A session without a record gets one.
func RecordModelSwitch(common, session, from, to string, at time.Time) error {
	if to == "" {
		return errors.New("session meta: model switch without a target model")
	}
	m, err := Read(common, session)
	if err != nil {
		m = nil // a corrupt file is replaced
	}
	if m == nil {
		m = &Meta{Session: session}
	}
	if from == "" {
		from = m.Model
	}
	m.Model, m.Updated = to, at.UTC()
	m.Switches = append(m.Switches, Switch{At: at.UTC(), From: from, To: to})
	if n := len(m.Switches); n > maxSwitches {
		m.Switches = m.Switches[n-maxSwitches:]
	}
	return write(common, *m)
}
