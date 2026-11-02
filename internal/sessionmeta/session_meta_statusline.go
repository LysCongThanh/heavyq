package sessionmeta

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/LysCongThanh/heavyq/internal/gitutil"
	"github.com/LysCongThanh/heavyq/internal/history"
)

// maxStatuslineInput bounds what ParseStatusline accepts.
const maxStatuslineInput = 1 << 20

// statusline is the part of the status-line stdin JSON heavyq reads
// (https://code.claude.com/docs/en/statusline).
type statusline struct {
	SessionID string `json:"session_id"`
	Cwd       string `json:"cwd"`
	Version   string `json:"version"`
	Model     struct {
		ID string `json:"id"`
	} `json:"model"`
	Workspace struct {
		CurrentDir string `json:"current_dir"`
	} `json:"workspace"`
	RateLimits struct {
		FiveHour *window `json:"five_hour"`
		SevenDay *window `json:"seven_day"`
	} `json:"rate_limits"`
}

type window struct {
	UsedPercentage *float64 `json:"used_percentage"`
}

func (w *window) pct() *float64 {
	if w == nil || w.UsedPercentage == nil || *w.UsedPercentage < 0 || *w.UsedPercentage > 100 {
		return nil
	}
	v := *w.UsedPercentage
	return &v
}

// Parsed is a status-line payload: the record fields and the directory the
// session works in (to find its repository).
type Parsed struct {
	Meta Meta
	Dir  string
}

// ParseStatusline reads a status-line stdin JSON. A missing or unsafe
// session_id is an error (there is nothing to key the record on); everything
// else is optional, because the schema gains and loses fields between versions.
func ParseStatusline(raw []byte) (Parsed, error) {
	if len(raw) > maxStatuslineInput {
		return Parsed{}, fmt.Errorf("status-line input over %d bytes", maxStatuslineInput)
	}
	var s statusline
	if err := json.Unmarshal(raw, &s); err != nil {
		return Parsed{}, fmt.Errorf("stdin is not status-line JSON: %w", err)
	}
	if !history.ValidTag(s.SessionID) {
		return Parsed{}, fmt.Errorf("status-line input has no valid session_id")
	}
	dir := s.Workspace.CurrentDir
	if dir == "" {
		dir = s.Cwd
	}
	return Parsed{Dir: dir, Meta: Meta{Session: s.SessionID, Model: s.Model.ID, Version: s.Version,
		FiveHourPct: s.RateLimits.FiveHour.pct(), SevenDayPct: s.RateLimits.SevenDay.pct()}}, nil
}

// RecordStatusline is the one function the status-line hook and the status
// line itself call: it parses the stdin JSON, finds the repository around its
// directory (fallbackDir when the payload has none) and stores the record. It
// returns what is stored. Nothing is written when the session's record
// already says the same and is under a minute old, which is what a 300 ms
// refresh cadence needs. Outside a git repository there is no place to keep
// state, and that is not an error: ok is false.
func RecordStatusline(raw []byte, fallbackDir string, now time.Time) (m Meta, ok bool, err error) {
	p, err := ParseStatusline(raw)
	if err != nil {
		return Meta{}, false, err
	}
	dir := p.Dir
	if dir == "" {
		dir = fallbackDir
	}
	if dir == "" {
		return p.Meta, false, nil
	}
	common, err := gitutil.CommonDir(dir)
	if err != nil {
		return p.Meta, false, nil
	}
	return RecordIn(common, p.Meta, now)
}

// RecordIn stores m (from ParseStatusline) in the repository with git common
// dir `common`, keeping the change points already recorded.
func RecordIn(common string, m Meta, now time.Time) (Meta, bool, error) {
	old, err := Read(common, m.Session)
	if err != nil {
		old = nil // a corrupt file is replaced
	}
	if old != nil {
		m.Switches = old.Switches
		if m.Model == "" {
			m.Model = old.Model
		}
		if m.Version == "" {
			m.Version = old.Version
		}
		if same(old, &m) && now.Sub(old.Updated) < rewriteEvery {
			return *old, true, nil
		}
	}
	m.Updated = now.UTC()
	if err := write(common, m); err != nil {
		return m, false, err
	}
	return m, true, nil
}

// same: nothing a reader sees differs (percentages compared to whole points).
func same(a, b *Meta) bool {
	return a.Model == b.Model && a.Version == b.Version && pctEq(a.FiveHourPct, b.FiveHourPct) && pctEq(a.SevenDayPct, b.SevenDayPct)
}

func pctEq(a, b *float64) bool {
	if a == nil || b == nil {
		return a == b
	}
	return int(*a) == int(*b)
}
