package statusline

import (
	"time"

	"github.com/LysCongThanh/heavyq/internal/sessionmeta"
)

// recordSessionMeta leaves the session's model, agent version and rate limits
// where the ledger and the flow state read them (internal/sessionmeta). The
// status line is the only complete source of them, so every refresh calls this;
// sessionmeta writes only when something changed. Best effort: the status line
// never fails on it, and a payload with neither model nor version carries nothing.
func recordSessionMeta(common string, in Input, now time.Time) {
	if in.Model == "" && in.Version == "" {
		return
	}
	m := sessionmeta.Meta{Session: in.SessionID, Model: in.Model, Version: in.Version}
	if in.FiveHour >= 0 {
		v := float64(in.FiveHour)
		m.FiveHourPct = &v
	}
	if in.SevenDay >= 0 {
		v := float64(in.SevenDay)
		m.SevenDayPct = &v
	}
	m.FiveHourResetsAt, m.SevenDayResetsAt = in.FiveHourResets, in.SevenDayResets
	_, _, _ = sessionmeta.RecordIn(common, m, now)
}
