package config

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
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
		"[queue]\npoll = \"1ns\"\nforce_after = \"-5m\"\n",
		"[local]\nmax_load_ratio = nan\n",
		"[mentor]\nmin_lift_pp = nan\n",
		"[local]\nlight_slots = 2\n",
		"[local]\nlegacy_lock_files = [\"\", \"\"]\n",
		"[local]\nenv_path = \"~/go/bin\"\n",
		"[[remote]]\nname = \"auto\"\nssh = \"box\"\n",
		"[[remote]]\nname = \"b\"\nssh = \"b\"\nwork_dir = \"~/.hq\"\n",
		"[[remote]]\nname = \"b\"\ntransport = [\"ssh\", \"b\"]\nssh = \"-oProxyCommand=x\"\n",
		"[hook]\nbinary = \"heavyq --config /x\"\n",
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
		if cfg.Queue.Poll.Duration < 50*time.Millisecond || cfg.Queue.ForceAfter.Duration <= 0 || cfg.Queue.LoadWeightMB <= 0 {
			t.Fatalf("accepted a bad queue value: %+v", cfg.Queue)
		}
		if p := cfg.Mentor.MinLiftPP; p != nil && !(*p >= 0 && *p <= 100) {
			t.Fatalf("accepted mentor.min_lift_pp %v", *p)
		}
		if b := cfg.Hook.Binary; b != "" && !filepath.IsAbs(b) && (strings.HasPrefix(b, "~") || strings.ContainsAny(b, "/ \t")) {
			t.Fatalf("accepted hook.binary %q", b)
		}
		if cfg.Local.SSH != "" || cfg.Local.EnvPath != "" || cfg.Local.CacheHook != "" || len(cfg.Local.Transport) > 0 {
			t.Fatalf("accepted remote-only keys in [local]: %+v", cfg.Local)
		}
		for _, f := range cfg.Local.LegacyLockFiles {
			if !filepath.IsAbs(f) {
				t.Fatalf("accepted legacy_lock_files entry %q", f)
			}
		}
		machines := []MachineConfig{cfg.Local}
		seen := map[string]bool{LocalName: true, RemoteAuto: true}
		heavy := cfg.Local.HeavySlots
		for _, r := range cfg.Remotes {
			if !SafeName.MatchString(r.Name) || seen[r.Name] {
				t.Fatalf("accepted remote name %q (seen: %v)", r.Name, seen)
			}
			seen[r.Name] = true
			if len(r.Transport) == 0 || r.GitURL == "" || strings.HasPrefix(r.GitURL, "-") || strings.HasPrefix(r.WorkDir, "~") {
				t.Fatalf("accepted remote with bad transport, git_url or work_dir: %+v", r)
			}
			heavy += r.HeavySlots
			machines = append(machines, r)
		}
		if heavy < 1 {
			t.Fatal("accepted a config where no machine has a heavy slot")
		}
		for _, m := range machines {
			total := m.HeavySlots + m.LightSlots
			if m.HeavySlots < 0 || m.LightSlots < 0 || total < 1 || total > 64 {
				t.Fatalf("accepted slots %d/%d on %q", m.HeavySlots, m.LightSlots, m.Name)
			}
			if !(m.MaxLoadRatio > 0 && m.MaxLoadRatio <= 64) || m.JobMB <= 0 || m.LightMB <= 0 || m.ReserveMB <= 0 {
				t.Fatalf("accepted bad budget on %q: %+v", m.Name, m)
			}
		}
	})
}
