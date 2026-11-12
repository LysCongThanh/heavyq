package config

import (
	"strings"
	"testing"
)

// A queue whose slot locks share a directory with other files is described by a glob of its own files.
func TestForeignSlotGlobsAccepted(t *testing.T) {
	for _, g := range []string{"/tmp/q-[0-9]*.lock", "/tmp/q-*.lock", "~/q/slot-[0-9][0-9].lock", "/q-[0-9]", "~/.q/s*.lock"} {
		if err := CheckSlotGlob(g); err != nil {
			t.Errorf("%q: %v", g, err)
		}
	}
	c, err := Load(writeConfig(t, observedBox+"[[foreign]]\nname = \"bashq\"\ntickets = \"/q\"\n"+
		"slot_dirs = { box = \"q/slots\" }\nslot_globs = { local = \"/tmp/bashq-[0-9]*.lock\", watch = \"~/bq-*.lock\" }\n"), true)
	if err != nil {
		t.Fatal(err)
	}
	if g := c.Foreign[0].SlotGlobs; g["local"] != "/tmp/bashq-[0-9]*.lock" || g["watch"] != "~/bq-*.lock" {
		t.Fatalf("slot_globs kept as written: %v", g)
	}
}

// A glob that could match outside its directory, name another queue's files wholesale or reach the shell is
// refused, with the key in the message.
func TestForeignSlotGlobsFailClosed(t *testing.T) {
	for g, want := range map[string]string{
		"q-[0-9]*.lock":       "absolute",
		"tmp/q-*.lock":        "absolute",
		"~q/q-*.lock":         "absolute",
		"/tmp/*/q-1.lock":     "directory part",
		"/tmp/q[0-9]/s*.lock": "directory part",
		"/tmp/../etc/q-*":     "directory part",
		"/tmp/a b/q-*":        "directory part",
		"/tmp/$(id)/q-*":      "directory part",
		"/tmp/~/q-*":          "directory part",
		"/tmp/":               "base name",
		"/tmp/.*":             "base name",
		"/tmp/.q-*.lock":      "base name",
		"/tmp/q-*-*.lock":     "one \"*\"",
		"/tmp/q-?.lock":       "base name may hold",
		"/tmp/q-[a-z].lock":   "base name may hold",
		"/tmp/q-[0-9.lock":    "base name may hold",
		"/tmp/q-1.lock;id":    "base name may hold",
		"/tmp/q-1.lock":       "no wildcard",
		"/tmp/*":              "plain part",
		"/tmp/[0-9]*":         "plain part",
	} {
		err := CheckSlotGlob(g)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: error %v must mention %q", g, err, want)
		}
	}
	base := observedBox + "[[foreign]]\nname = \"bashq\"\ntickets = \"/q\"\n"
	for want, body := range map[string]string{
		"not a configured machine": base + "slot_globs = { nowhere = \"/tmp/q-*.lock\" }\n",
		"slot_dirs too":            base + "slot_dirs = { box = \"q\" }\nslot_globs = { box = \"/tmp/q-*.lock\" }\n",
		"slot_globs":               base + "slot_globs = { box = \"/tmp/*\" }\n",
	} {
		if _, err := Load(writeConfig(t, body), true); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: error %v must mention %q", body, err, want)
		}
	}
}
