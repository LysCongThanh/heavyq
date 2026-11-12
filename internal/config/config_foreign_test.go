package config

import (
	"slices"
	"strings"
	"testing"
)

const observedBox = `
[[remote]]
name = "box"
ssh = "box"
[[remote]]
name = "watch"
ssh = "watch"
observe_only = true
`

// An observe_only remote is watched by `heavyq fleet` and never offered to the scheduler: it is not in
// Remotes, so placement, --remote and [proof] cannot pick it.
func TestObserveOnlyRemoteIsNeverARunTarget(t *testing.T) {
	c, err := Load(writeConfig(t, observedBox), true)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Remotes) != 1 || c.Remotes[0].Name != "box" {
		t.Fatalf("remotes = %+v", c.Remotes)
	}
	if len(c.Observed) != 1 || c.Observed[0].Name != "watch" || c.Observed[0].HeavySlots != 0 || len(c.Observed[0].Transport) == 0 {
		t.Fatalf("observed = %+v", c.Observed)
	}
	if _, ok := c.Machine("watch"); ok {
		t.Fatal("an observe_only remote must not resolve as a run target")
	}
	if got := c.FleetMachineNames(); !slices.Equal(got, []string{"local", "box", "watch"}) {
		t.Fatalf("fleet machines = %v", got)
	}
	if _, err := Load(writeConfig(t, observedBox+"[proof]\nremote = \"watch\"\n"), true); err == nil {
		t.Fatal("[proof] remote must refuse an observe_only machine")
	}
}

func TestObserveOnlyRefusesRunKeys(t *testing.T) {
	for key, line := range map[string]string{
		"heavy_slots": "heavy_slots = 2", "light_slots": "light_slots = 1", "git_url": `git_url = "x:y"`,
		"cache_hook": `cache_hook = "true"`, "env_path": `env_path = "~/bin"`, "job_mb": "job_mb = 10",
		"allow_untrusted_exit": "allow_untrusted_exit = true",
	} {
		_, err := Load(writeConfig(t, "[[remote]]\nname = \"watch\"\nssh = \"watch\"\nobserve_only = true\n"+line+"\n"), true)
		if err == nil || !strings.Contains(err.Error(), key) {
			t.Errorf("%s: error %v must name the key", key, err)
		}
	}
	if _, err := Load(writeConfig(t, "[[remote]]\nname = \"watch\"\nobserve_only = true\n"), true); err == nil {
		t.Error("an observe_only remote still needs ssh or transport")
	}
}

const foreignOK = `
[[foreign]]
name = "bashq"
history = "/var/tmp/q/history.tsv"
tickets = "/var/tmp/q"
infra_exits = [97, 124]
machines_dir = "/var/tmp/q/machines.d"
slot_dirs = { box = "q/slots", local = "/var/tmp/q/slots" }
aliases = { "build-1" = "local" }
`

func TestForeignQueueDefaults(t *testing.T) {
	c, err := Load(writeConfig(t, observedBox+foreignOK), true)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Foreign) != 1 {
		t.Fatalf("foreign = %+v", c.Foreign)
	}
	f := c.Foreign[0]
	if f.Format != "tsv" || !slices.Equal(f.Columns, ForeignColumns) {
		t.Fatalf("format/columns default: %q %v", f.Format, f.Columns)
	}
	if f.MachineName("build-1") != "local" || f.MachineName("box") != "box" {
		t.Fatal("aliases map the foreign name to the heavyq machine, other names stay")
	}
}

// Every unsafe or unknown value is refused with the key in the message: a typo must not observe the wrong file.
func TestForeignQueueFailsClosed(t *testing.T) {
	base := "[[foreign]]\nname = \"bashq\"\n"
	for want, body := range map[string]string{
		"history or tickets": base,
		"name":               "[[foreign]]\nname = \"heavyq\"\ntickets = \"/q\"\n",
		"twice":              base + "tickets = \"/q\"\n" + base + "tickets = \"/r\"\n",
		"format":             base + "tickets = \"/q\"\nformat = \"csv\"\n",
		"unknown column":     base + "history = \"/q/h.tsv\"\ncolumns = [\"end_epoch\", \"machine\", \"exit\", \"color\"]\n",
		"listed twice":       base + "history = \"/q/h.tsv\"\ncolumns = [\"end_epoch\", \"machine\", \"exit\", \"exit\"]\n",
		"needs the column":   base + "history = \"/q/h.tsv\"\ncolumns = [\"machine\", \"exit\"]\n",
		"absolute":           base + "history = \"q/h.tsv\"\n",
		"infra_exits":        base + "tickets = \"/q\"\ninfra_exits = [0]\n",
		"slot_dirs":          base + "tickets = \"/q\"\nslot_dirs = { nowhere = \"/s\" }\n",
		"unsafe":             base + "tickets = \"/q\"\nslot_dirs = { local = \"/s/$(id)\" }\n",
		"aliases":            base + "tickets = \"/q\"\naliases = { x = \"nowhere\" }\n",
		"machines_dir":       base + "tickets = \"/q\"\nmachines_dir = \"rel/dir\"\n",
	} {
		if _, err := Load(writeConfig(t, body), true); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: error %v must mention %q", body, err, want)
		}
	}
}

func TestLocalObserveOnlyFailsClosed(t *testing.T) {
	_, err := Load(writeConfig(t, "[local]\nobserve_only = true\n"), true)
	if err == nil || !strings.Contains(err.Error(), "observe_only") {
		t.Fatalf("[local] observe_only must be refused, got %v", err)
	}
}
