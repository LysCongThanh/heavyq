package config

import (
	"strings"
	"testing"
)

func TestMentorDefaults(t *testing.T) {
	c, err := Load("", false)
	if err != nil {
		t.Fatal(err)
	}
	if c.MentorHoldoutPct() != 20 || !c.MentorAutoMute() || c.MentorMinEvents() != 200 || c.MentorMinLiftPP() != 10 {
		t.Fatalf("defaults: %d %v %d %v", c.MentorHoldoutPct(), c.MentorAutoMute(), c.MentorMinEvents(), c.MentorMinLiftPP())
	}
}

func TestMentorSection(t *testing.T) {
	c, err := Load(writeConfig(t, "[mentor]\nholdout_pct = 0\nauto_mute = false\nmin_events = 50\nmin_lift_pp = 5.5\n"), true)
	if err != nil {
		t.Fatal(err)
	}
	if c.MentorHoldoutPct() != 0 || c.MentorAutoMute() || c.MentorMinEvents() != 50 || c.MentorMinLiftPP() != 5.5 {
		t.Fatalf("values: %d %v %d %v", c.MentorHoldoutPct(), c.MentorAutoMute(), c.MentorMinEvents(), c.MentorMinLiftPP())
	}
	if c, err := Load(writeConfig(t, "[mentor]\nholdout_pct = 100\n"), true); err != nil || c.MentorHoldoutPct() != 100 {
		t.Fatalf("shadow mode (100) is valid: %v", err)
	}
}

func TestMentorSectionIsValidated(t *testing.T) {
	cases := map[string]string{
		"[mentor]\nholdout_pct = 101\n": "holdout_pct",
		"[mentor]\nholdout_pct = -1\n":  "holdout_pct",
		"[mentor]\nmin_events = -3\n":   "min_events",
		"[mentor]\nmin_lift_pp = 150\n": "min_lift_pp",
		"[mentor]\nHoldout_Pct = 20\n":  "Holdout_Pct",
	}
	for body, want := range cases {
		if _, err := Load(writeConfig(t, body), true); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: error %v must mention %q", body, err, want)
		}
	}
}
