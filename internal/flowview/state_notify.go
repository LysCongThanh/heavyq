package flowview

import (
	"strings"
	"time"
	"unicode/utf8"

	"github.com/LysCongThanh/heavyq/internal/forecast"
	"github.com/LysCongThanh/heavyq/internal/notify"
	"github.com/LysCongThanh/heavyq/internal/redact"
)

// maxNeedText bounds one need's line in a notice.
const maxNeedText = 100

// NotifyFacts turns a state into what the notifier compares: the needs with a
// key that stays the same while the need is open, the forecast verdict and
// whether everything is proven. A reason may quote a command, so it is redacted.
func (s *State) NotifyFacts(now time.Time) notify.Facts {
	fc := s.Forecast(now)
	f := notify.Facts{
		Now: now, Updated: s.Updated,
		AtRisk: fc.Status == forecast.AtRisk, AtRiskWhy: fc.Reason, ETA: fc.ETA,
		AllProven: s.AllProven(), Proven: s.Items.Proven, Total: s.Items.Total,
	}
	for _, n := range s.NeedsYou {
		who := n.Item
		if who == "" {
			who = n.Lane
		}
		text := who
		if n.Reason != "" {
			text += ": " + redact.Command(n.Reason)
		}
		if utf8.RuneCountInString(text) > maxNeedText {
			text = string([]rune(text)[:maxNeedText-3]) + "..."
		}
		key := strings.Join([]string{n.Item, n.Lane, n.Session, n.Since.UTC().Format(time.RFC3339)}, "|")
		f.Needs = append(f.Needs, notify.Need{Key: key, Text: text})
	}
	return f
}
