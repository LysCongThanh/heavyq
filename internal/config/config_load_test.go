package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestDefaultsWithoutFile(t *testing.T) {
	c, err := Load(filepath.Join(t.TempDir(), "missing.toml"), false)
	if err != nil {
		t.Fatal(err)
	}
	if c.Local.HeavySlots != 2 || c.Local.LightSlots != 1 {
		t.Fatalf("slots = %d/%d", c.Local.HeavySlots, c.Local.LightSlots)
	}
	if c.Queue.ForceAfter.Duration != 15*time.Minute || c.Queue.Poll.Duration != 2*time.Second {
		t.Fatalf("durations: %+v", c.Queue)
	}
	if !c.GateEnabled() || c.AutoEnabled() {
		t.Fatal("gate must default on; auto needs remotes")
	}
}

func TestMissingRequiredFileFails(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "missing.toml"), true); err == nil {
		t.Fatal("expected error for explicit missing config")
	}
}

func TestRemoteDefaults(t *testing.T) {
	p := writeConfig(t, `
[queue]
force_after = "5m"
[local]
legacy_lock_files = ["/tmp/a.lock", "/tmp/b.lock", "/tmp/c.lock"]
[[remote]]
name = "box"
ssh = "box"
light_slots = 2
`)
	c, err := Load(p, true)
	if err != nil {
		t.Fatal(err)
	}
	if c.Local.HeavySlots != 3 {
		t.Fatalf("legacy files must define heavy slots, got %d", c.Local.HeavySlots)
	}
	r, ok := c.Machine("box")
	if !ok {
		t.Fatal("remote not found")
	}
	if got := strings.Join(r.Transport, " "); got != "ssh -o BatchMode=yes -o ConnectTimeout=10 box" {
		t.Fatalf("transport = %q", got)
	}
	if r.GitURL != "box:.heavyq/git" || r.HeavySlots != 0 || r.LightSlots != 2 {
		t.Fatalf("remote = %+v", r)
	}
	if c.Queue.ForceAfter.Duration != 5*time.Minute {
		t.Fatal("force_after not decoded")
	}
}

func TestRejectsBadConfigs(t *testing.T) {
	cases := map[string]string{
		"reserved name":    "[[remote]]\nname = \"local\"\nssh = \"x\"\n",
		"duplicate":        "[[remote]]\nname = \"a\"\nssh = \"a\"\n[[remote]]\nname = \"a\"\nssh = \"a\"\n",
		"ssh with options": "[[remote]]\nname = \"a\"\nssh = \"-oProxyCommand=evil a\"\n",
		"no transport":     "[[remote]]\nname = \"a\"\n",
		"transport no git": "[[remote]]\nname = \"a\"\ntransport = [\"sh\", \"-c\"]\n",
		"unsafe work dir":  "[[remote]]\nname = \"a\"\nssh = \"a\"\nwork_dir = \"$(rm -rf x)\"\n",
		"negative ratio":   "[local]\nmax_load_ratio = -1\n",
		"too many slots":   "[local]\nheavy_slots = 100\n",
		"bad duration":     "[queue]\npoll = \"soon\"\n",
		"bad base ref":     "[queue]\nbase_ref = \"../x\"\n",
		"legacy on remote": "[[remote]]\nname = \"a\"\nssh = \"a\"\nlegacy_lock_files = [\"/x\"]\n",
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Load(writeConfig(t, body), true); err == nil {
				t.Fatalf("expected error for %q", body)
			}
		})
	}
}

func TestAutoCanBeDisabledByEnv(t *testing.T) {
	c, err := Load(writeConfig(t, "[[remote]]\nname = \"a\"\nssh = \"a\"\n"), true)
	if err != nil {
		t.Fatal(err)
	}
	if !c.AutoEnabled() {
		t.Fatal("auto should be on with a remote")
	}
	t.Setenv("HEAVYQ_AUTO", "0")
	if c.AutoEnabled() {
		t.Fatal("HEAVYQ_AUTO=0 must disable auto")
	}
}

// The documented example must stay loadable.
func TestExampleConfigLoads(t *testing.T) {
	c, err := Load(filepath.Join("..", "..", "examples", "config.toml"), true)
	if err != nil {
		t.Fatal(err)
	}
	if r, ok := c.Machine("buildbox"); !ok || r.CacheHook == "" || r.GitURL != "buildbox:.heavyq/git" {
		t.Fatalf("example remote: %+v", r)
	}
	if len(c.Hook.Rules) != 1 || !c.Hook.Rules[0].Cache || c.Cache.TTL.Duration != 24*time.Hour {
		t.Fatalf("example hook/cache: %+v %+v", c.Hook, c.Cache)
	}
}

func TestHookAndCacheSections(t *testing.T) {
	c, err := Load(writeConfig(t, `
[hook]
permission = "allow"
max_wait = "90s"
[[hook.rule]]
match = ["bun test", "deno task check"]
light = true
cache = true
[cache]
env = ["CI", "NODE_ENV"]
max_size_mb = 10
max_entry_mb = 50
`), true)
	if err != nil {
		t.Fatal(err)
	}
	if !c.HookEnabled() || !c.HookDefaultRules() || c.Hook.MaxWait.Duration != 90*time.Second {
		t.Fatalf("hook: %+v", c.Hook)
	}
	if r := c.Hook.Rules[0]; r.Priority != 4 || !r.Light || !r.Cache {
		t.Fatalf("rule: %+v", r)
	}
	if c.Cache.TTL.Duration != 24*time.Hour || c.Cache.MaxEntryMB != 10 || c.Cache.CacheFailures {
		t.Fatalf("cache: %+v", c.Cache)
	}
	for _, bad := range []string{
		"[hook]\npermission = \"ask\"",
		"[hook]\nbinary = \"heavyq; rm x\"",
		"[[hook.rule]]\nmatch = []",
		"[[hook.rule]]\nmatch = [\"npm test && x\"]",
		"[[hook.rule]]\nmatch = [\"npm test\"]\npriority = 12",
		"[[hook.rule]]\nmatch = [\"npm test\"]\nlane = \"a b\"",
		"[cache]\nenv = [\"BAD-NAME\"]",
		"[cache]\nmax_size_mb = -1",
	} {
		if _, err := Load(writeConfig(t, bad), true); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
}
