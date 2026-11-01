package config

import (
	"reflect"
	"strings"
	"testing"
)

// maxFuzzInput bounds every fuzz input so one execution stays fast.
const maxFuzzInput = 64 << 10

// FuzzParse feeds arbitrary bytes to the unexported parse that Load uses for
// ~/.config/heavyq/config.toml. It must never panic, must be deterministic,
// must name the file in every error, and everything it accepts must satisfy
// the documented limits (fail closed: nothing invalid slips through).
func FuzzParse(f *testing.F) {
	for _, s := range []string{
		"",
		"[queue]\nforce_after = \"5m\"\n[local]\nlegacy_lock_files = [\"/tmp/a.lock\", \"/tmp/b.lock\"]\n[[remote]]\nname = \"box\"\nssh = \"box\"\nlight_slots = 2\n",
		"[hook]\npermission = \"allow\"\nmax_wait = \"90s\"\n[[hook.rule]]\nmatch = [\"bun test\", \"deno task check\"]\nlight = true\ncache = true\n[cache]\nenv = [\"CI\", \"NODE_ENV\"]\nmax_size_mb = 10\nmax_entry_mb = 50\n",
		"[queue]\nbogus = 1\n",
		"[queue]\nbase_ref = \"../x\"\n",
		"[local]\nmax_load_ratio = -1\n",
		"[local]\nheavy_slots = 100\n",
		"[[remote]]\nname = \"local\"\nssh = \"x\"\n",
		"[[remote]]\nname = \"a\"\nssh = \"a\"\n[[remote]]\nname = \"a\"\nssh = \"a\"\n",
		"[[remote]]\nname = \"a\"\nssh = \"-oProxyCommand=evil a\"\n",
		"[[remote]]\nname = \"a\"\nssh = \"a\"\nwork_dir = \"$(rm -rf x)\"\n",
		"[LOCAL]\nheavy_slots = 1\n[local]\nheavy_slots = 4\n",
		"[local]\nHeavy_Slots = 3\n",
		"[[REMOTE]]\nname = \"a\"\nssh = \"a\"\n",
		"[queue]\npoll = \"soon\"\n",
		"name = = =\n[[",
		"\xff\xfe[local]",
	} {
		f.Add([]byte(s))
	}
	const path = "/home/u/.config/heavyq/config.toml"
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > maxFuzzInput {
			t.Skip("input over the fuzz bound")
		}
		cfg, err := parse(path, data)
		again, err2 := parse(path, data)
		if (err == nil) != (err2 == nil) || !reflect.DeepEqual(cfg, again) {
			t.Fatalf("parse is not deterministic for %q", data)
		}
		if err != nil {
			if cfg != nil || !strings.Contains(err.Error(), path) {
				t.Fatalf("error must come with no config and name the file: cfg=%v err=%v", cfg, err)
			}
			return
		}
		if cfg.Path != path || cfg.Local.Name != LocalName {
			t.Fatalf("config identity lost: path=%q local=%q", cfg.Path, cfg.Local.Name)
		}
		if cfg.Queue.Poll.Duration <= 0 || cfg.Queue.ForceAfter.Duration <= 0 || cfg.Queue.LoadWeightMB <= 0 {
			t.Fatalf("accepted a non-positive queue default: %+v", cfg.Queue)
		}
		machines := []MachineConfig{cfg.Local}
		seen := map[string]bool{LocalName: true}
		for _, r := range cfg.Remotes {
			if !SafeName.MatchString(r.Name) || seen[r.Name] {
				t.Fatalf("accepted remote name %q (seen: %v)", r.Name, seen)
			}
			seen[r.Name] = true
			if len(r.Transport) == 0 || r.GitURL == "" {
				t.Fatalf("accepted remote without transport or git_url: %+v", r)
			}
			machines = append(machines, r)
		}
		for _, m := range machines {
			total := m.HeavySlots + m.LightSlots
			if m.HeavySlots < 0 || m.LightSlots < 0 || total < 1 || total > 64 {
				t.Fatalf("accepted slots %d/%d on %q", m.HeavySlots, m.LightSlots, m.Name)
			}
			if m.MaxLoadRatio <= 0 || m.MaxLoadRatio > 64 || m.JobMB <= 0 || m.LightMB <= 0 || m.ReserveMB <= 0 {
				t.Fatalf("accepted bad budget on %q: %+v", m.Name, m)
			}
		}
	})
}
