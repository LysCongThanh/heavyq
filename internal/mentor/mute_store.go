package mentor

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Who muted a rule.
const (
	MutedByUser = "user"
	MutedByAuto = "auto"
)

// MuteEntry is the mute state of one rule. After `unmute` the entry stays
// with Muted false: its At is where the auto-mute judgement starts counting
// again, so a rule the user turned back on is not muted again by the old
// events that muted it.
type MuteEntry struct {
	Muted  bool      `json:"muted"`
	By     string    `json:"by"`
	At     time.Time `json:"at"`
	Reason string    `json:"reason,omitempty"`
}

type muteFile struct {
	V     int                  `json:"v"`
	Rules map[string]MuteEntry `json:"rules"`
}

func (s *Store) mutePath() string { return filepath.Join(s.Dir, "mute.json") }

// Mutes reads mute.json. A missing or unreadable file means nothing is muted.
func (s *Store) Mutes() map[string]MuteEntry {
	b, err := readFileIfExists(s.mutePath())
	if err != nil || b == nil {
		return map[string]MuteEntry{}
	}
	var f muteFile
	if json.Unmarshal(b, &f) != nil || f.Rules == nil {
		return map[string]MuteEntry{}
	}
	return f.Rules
}

// IsMuted reports whether rule is muted.
func (s *Store) IsMuted(rule string) bool { return s.Mutes()[rule].Muted }

// SetMute mutes or unmutes rule. rule must be a known rule: an unknown name
// is refused, never stored.
func (s *Store) SetMute(rule string, muted bool, by, reason string, now time.Time) error {
	if !KnownRule(rule) {
		return fmt.Errorf("unknown mentor rule %q (known: %v)", rule, Rules())
	}
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return err
	}
	rules := s.Mutes()
	rules[rule] = MuteEntry{Muted: muted, By: by, At: now.UTC(), Reason: reason}
	b, err := json.Marshal(muteFile{V: 1, Rules: rules})
	if err != nil {
		return err
	}
	return writeFileAtomic(s.mutePath(), b)
}

// EvaluateAutoMute applies policy p to the log and mutes every rule that
// fails it, returning the rules it muted. Only events after a rule's last
// unmute count. It reads the whole log, so hooks call it only after an event
// resolved, never per tool call.
func (s *Store) EvaluateAutoMute(p AutoMute, now time.Time) ([]string, error) {
	if !p.Enabled {
		return nil, nil
	}
	entries, err := s.ReadEntries()
	if err != nil {
		return nil, err
	}
	var muted []string
	for _, m := range autoMutes(p, entries, s.Mutes()) {
		if err := s.SetMute(m.rule, true, MutedByAuto, m.why, now); err != nil {
			return muted, err
		}
		muted = append(muted, m.rule)
	}
	return muted, nil
}

// autoMute is a rule policy p mutes, and why.
type autoMute struct{ rule, why string }

// autoMutes is the decision of EvaluateAutoMute on entries and the current
// mutes, without writing anything.
func autoMutes(p AutoMute, entries []Entry, mutes map[string]MuteEntry) []autoMute {
	if !p.Enabled {
		return nil
	}
	var out []autoMute
	for _, rule := range Rules() {
		m := mutes[rule]
		if m.Muted {
			continue
		}
		var rs RuleStats
		for _, c := range ComputeStats(entries, m.At) { // m.At is zero if never touched
			if c.Rule == rule {
				rs = c
			}
		}
		if mute, why := p.ShouldMute(rs); mute {
			out = append(out, autoMute{rule: rule, why: why})
		}
	}
	return out
}

// StatsWithMutes is ComputeStats over the whole log plus the mute state of
// each rule, which is what `heavyq mentor stats` shows.
func (s *Store) StatsWithMutes() ([]RuleStats, error) {
	entries, err := s.ReadEntries()
	if err != nil {
		return nil, err
	}
	stats := ComputeStats(entries, time.Time{})
	mutes := s.Mutes()
	for i := range stats {
		if m := mutes[stats[i].Rule]; m.Muted {
			stats[i].Muted, stats[i].MutedBy, stats[i].MutedWhy = true, m.By, m.Reason
		}
	}
	return stats, nil
}
