package config

import (
	"strings"
	"testing"
)

// Each slot key has its own documented default (2 heavy / 1 light): setting
// only light_slots must not silently drop every heavy slot, which leaves
// heavy runs waiting forever with "no slot kind usable by this ticket".
func TestLocalSlotKeysDefaultOneByOne(t *testing.T) {
	cases := []struct {
		body         string
		heavy, light int
	}{
		{"[local]\nlight_slots = 3\n", 2, 3},
		{"[local]\nheavy_slots = 4\n", 4, 1},
		{"[local]\nheavy_slots = 0\nlight_slots = 2\n[[remote]]\nname = \"b\"\nssh = \"b\"\n", 0, 2},
		{"[local]\nlegacy_lock_files = [\"/var/lock/a.lock\"]\n", 1, 0},
	}
	for _, tc := range cases {
		c, err := Load(writeConfig(t, tc.body), true)
		if err != nil {
			t.Fatalf("%q: %v", tc.body, err)
		}
		if c.Local.HeavySlots != tc.heavy || c.Local.LightSlots != tc.light {
			t.Errorf("%q: slots %d/%d, want %d/%d", tc.body, c.Local.HeavySlots, c.Local.LightSlots, tc.heavy, tc.light)
		}
	}
}

// A config where no machine has a heavy slot can never run a heavy job.
func TestRefusesConfigWithoutAnyHeavySlot(t *testing.T) {
	for _, body := range []string{
		"[local]\nheavy_slots = 0\nlight_slots = 2\n",
		"[local]\nheavy_slots = 0\nlight_slots = 1\n[[remote]]\nname = \"b\"\nssh = \"b\"\nlight_slots = 2\n",
	} {
		if _, err := Load(writeConfig(t, body), true); err == nil || !strings.Contains(err.Error(), "heavy_slots") {
			t.Errorf("%q: err = %v", body, err)
		}
	}
}

// legacy_lock_files are opened as given by every heavyq process: a relative
// path is a different file per working directory, "" fails every claim, and
// a repeated path or slot name is one slot counted twice.
func TestLegacyLockFilesMustBeDistinctAbsolutePaths(t *testing.T) {
	cases := map[string]string{
		"relative":       `["build-1.lock", "build-2.lock"]`,
		"home relative":  `["~/build.lock"]`,
		"empty":          `["", ""]`,
		"duplicate":      `["/var/lock/a.lock", "/var/lock/a.lock"]`,
		"same file":      `["/var/lock/a.lock", "/var/lock/./a.lock"]`,
		"same slot name": `["/a/build.lock", "/b/build.lock"]`,
	}
	for name, list := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := Load(writeConfig(t, "[local]\nlegacy_lock_files = "+list+"\n"), true)
			if err == nil || !strings.Contains(err.Error(), "legacy_lock_files") {
				t.Fatalf("%s: err = %v", list, err)
			}
		})
	}
	_, err := Load(writeConfig(t, "[local]\nlight_slots = 1\nlegacy_lock_files = [\"/var/lock/light-1.lock\"]\n"), true)
	if err == nil || !strings.Contains(err.Error(), "light-1") {
		t.Fatalf("a legacy file named like a light slot: err = %v", err)
	}
}

// Remote-only keys under [local] decode into known fields, so they were
// neither warned about nor used: the operator believed local runs got them.
func TestRemoteOnlyKeysAreRefusedInLocal(t *testing.T) {
	for key, line := range map[string]string{
		"ssh":        `ssh = "ws"`,
		"transport":  `transport = ["sh", "-c"]`,
		"work_dir":   `work_dir = "x"`,
		"git_url":    `git_url = "/srv/git"`,
		"cache_hook": `cache_hook = "true"`,
		"env_path":   `env_path = "~/go/bin"`,
		"name":       `name = "ws"`,
		// observe_only is a [[remote]] key too, refused by the same check.
		"observe_only": `observe_only = true`,
	} {
		_, err := Load(writeConfig(t, "[local]\n"+line+"\n"), true)
		if err == nil || !strings.Contains(err.Error(), key) {
			t.Errorf("[local] %s: err = %v", line, err)
		}
	}
	if _, err := Load(writeConfig(t, "[local]\nname = \"local\"\n"), true); err != nil {
		t.Fatalf("[local] name = \"local\" is harmless: %v", err)
	}
}
