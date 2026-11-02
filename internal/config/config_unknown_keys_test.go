package config

import (
	"strings"
	"testing"
)

// #128: a config written for a newer heavyq (a new key, a new table) must not
// stop an older one. The unknown keys are kept, in file order, with their lines.
func TestUnknownKeysAreIgnoredAndListed(t *testing.T) {
	body := `[queue]
poll = "3s"
future_knob = 1

[future]
a = 1
[future.deeper]
b = 2

[[remote]]
name = "box"
ssh = "box"
new_remote_key = true
`
	p := writeConfig(t, body)
	c, err := Load(p, true)
	if err != nil {
		t.Fatalf("a config with unknown keys must load: %v", err)
	}
	if c.Queue.Poll.Duration.String() != "3s" {
		t.Errorf("known keys still apply: poll = %v", c.Queue.Poll.Duration)
	}
	if r, ok := c.Machine("box"); !ok || r.SSH != "box" {
		t.Errorf("known remote keys still apply: %+v", r)
	}
	var got []string
	for _, u := range c.Unknown {
		got = append(got, u.String())
	}
	want := "queue.future_knob (line 3), future (line 5), remote.new_remote_key (line 13)"
	if g := strings.Join(got, ", "); g != want {
		t.Errorf("unknown = %q, want %q", g, want)
	}
	w := c.UnknownKeysWarning()
	if !strings.Contains(w, p) || !strings.Contains(w, "3 key(s)") || !strings.Contains(w, "future (line 5)") || strings.Count(w, "\n") != 0 {
		t.Errorf("warning = %q", w)
	}
}

func TestNoUnknownKeysNoWarning(t *testing.T) {
	c, err := Load(writeConfig(t, "[queue]\npoll = \"3s\"\n"), true)
	if err != nil || len(c.Unknown) != 0 || c.UnknownKeysWarning() != "" {
		t.Fatalf("clean config: %+v, %v", c.Unknown, err)
	}
}

// An unknown key does not hide a value that is wrong: validation still runs.
func TestUnknownKeysDoNotSkipValidation(t *testing.T) {
	if _, err := Load(writeConfig(t, "[future]\nx = 1\n[local]\nmax_load_ratio = -1\n"), true); err == nil {
		t.Fatal("an invalid known value was accepted")
	}
	// An upper-case key that does decode into the schema stays an error (the merge hazard), unknown ones do not.
	if _, err := Load(writeConfig(t, "[LOCAL]\nheavy_slots = 4\n"), true); err == nil {
		t.Fatal("[LOCAL] was accepted")
	}
	if c, err := Load(writeConfig(t, "[FutureSection]\nKnob = 1\n"), true); err != nil || len(c.Unknown) == 0 {
		t.Fatalf("an unknown upper-case table should only be listed: %+v, %v", c, err)
	}
}
