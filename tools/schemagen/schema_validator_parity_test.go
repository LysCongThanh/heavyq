package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/LysCongThanh/heavyq/internal/config"
)

// loadConfig runs heavyq's own loader on doc, written to a real config file.
func loadConfig(t *testing.T, doc string) error {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := config.Load(path, true)
	return err
}

const box = "[[remote]]\nname = \"box\"\nssh = \"box\"\n"

// What heavyq refuses when it loads the user config, config.schema.json refuses too, so an editor or
// `taplo lint` shows the error before a run does; and what heavyq accepts, the schema accepts.
func TestUserConfigSchemaRefusesWhatTheLoaderRefuses(t *testing.T) {
	inRepoRoot(t)
	cfg := compile(t, "config.schema.json")
	for name, doc := range map[string]string{
		"negative heavy slots":       "[local]\nheavy_slots = -1\n",
		"too many slots":             "[local]\nheavy_slots = 65\n",
		"negative light slots":       box + "light_slots = -1\n",
		"load ratio above 64":        "[local]\nmax_load_ratio = 65.0\n",
		"negative load ratio":        box + "max_load_ratio = -0.5\n",
		"negative job budget":        "[local]\njob_mb = -1\n",
		"negative light budget":      box + "light_mb = -1\n",
		"negative reserve":           "[local]\nreserve_mb = -1\n",
		"holdout above 100":          "[mentor]\nholdout_pct = 150\n",
		"negative holdout":           "[mentor]\nholdout_pct = -1\n",
		"lift above 100":             "[mentor]\nmin_lift_pp = 100.5\n",
		"negative min events":        "[mentor]\nmin_events = -1\n",
		"negative cache size":        "[cache]\nmax_size_mb = -1\n",
		"negative cache entry":       "[cache]\nmax_entry_mb = -1\n",
		"bad cache env name":         "[cache]\nenv = [\"NODE-ENV\"]\n",
		"lane with a space":          "[[hook.rule]]\nmatch = [\"bun test\"]\nlane = \"my lane\"\n",
		"match with a semicolon":     "[[hook.rule]]\nmatch = [\"bun;test\"]\n",
		"blank match":                "[[hook.rule]]\nmatch = [\"  \"]\n",
		"hook binary metacharacters": "[hook]\nbinary = \"heavyq;rm\"\n",
		"base ref without remote":    "[queue]\nbase_ref = \"main\"\n",
		"base ref with ..":           "[queue]\nbase_ref = \"origin/../main\"\n",
		"remote without name":        "[[remote]]\nssh = \"box\"\n",
		"remote named local":         "[[remote]]\nname = \"local\"\nssh = \"box\"\n",
		"remote name with a space":   "[[remote]]\nname = \"a box\"\nssh = \"box\"\n",
		"ssh option as alias":        "[[remote]]\nname = \"box\"\nssh = \"-oProxyCommand=x\"\n",
		"remote without ssh":         "[[remote]]\nname = \"box\"\n",
		"transport without git_url":  "[[remote]]\nname = \"box\"\ntransport = [\"ssh\", \"box\"]\n",
		"work_dir with a space":      box + "work_dir = \"a b\"\n",
		"work_dir with ..":           box + "work_dir = \"../x\"\n",
		"env_path with $":            box + "env_path = \"$HOME/bin\"\n",
		"env_path of colons only":    box + "env_path = \"::\"\n",
		"legacy locks on a remote":   box + "legacy_lock_files = [\"/tmp/x.lock\"]\n",
		"need_gh repo listed twice":  "[run]\nneed_gh_repos = [\"app\", \"app\"]\n",
		"remote named auto":          "[[remote]]\nname = \"auto\"\nssh = \"box\"\n",
		"relative legacy lock":       "[local]\nlegacy_lock_files = [\"build-1.lock\"]\n",
		"legacy lock listed twice":   "[local]\nlegacy_lock_files = [\"/tmp/a.lock\", \"/tmp/a.lock\"]\n",
		"git_url as an option":       "[[remote]]\nname = \"box\"\ntransport = [\"ssh\", \"box\"]\ngit_url = \"--upload-pack=x\"\n",
		"work_dir of another home":   box + "work_dir = \"~other/x\"\n",
		"work_dir of the home":       box + "work_dir = \"~/\"\n",
		"binary with an argument":    "[hook]\nbinary = \"heavyq --x\"\n",
		"binary relative path":       "[hook]\nbinary = \"bin/heavyq\"\n",
		"local observe_only":         "[local]\nobserve_only = true\n",
		"observe-only with slots":    "[[remote]]\nname = \"watch\"\nssh = \"watch\"\nobserve_only = true\nheavy_slots = 2\n",
		"observe-only with git_url":  "[[remote]]\nname = \"watch\"\nssh = \"watch\"\nobserve_only = true\ngit_url = \"watch:x\"\n",
	} {
		if err := loadConfig(t, doc); err == nil {
			t.Errorf("%s: heavyq accepts it; this case belongs in the valid list", name)
			continue
		}
		if err := cfg.Validate(tomlDoc(t, doc)); err == nil {
			t.Errorf("%s: heavyq refuses it, config.schema.json accepts it", name)
		}
	}
	for name, doc := range map[string]string{
		"defaults":               "",
		"zero means default":     "[local]\nmax_load_ratio = 0.0\njob_mb = 0\n[mentor]\nmin_events = 0\n",
		"explicit zero slots":    "[local]\nlight_slots = 0\n" + box + "heavy_slots = 0\nlight_slots = 1\n",
		"edges":                  "[local]\nheavy_slots = 64\nlight_slots = 0\n[mentor]\nholdout_pct = 100\nmin_lift_pp = 0.0\n",
		"work_dir under ~/":      box + "work_dir = \"~/cache/heavyq\"\n",
		"binary under ~/":        "[hook]\nbinary = \"~/bin/heavyq\"\n",
		"binary on PATH":         "[hook]\nbinary = \"heavyq\"\n",
		"observe-only by ssh":    "[[remote]]\nname = \"watch\"\nssh = \"watch\"\nobserve_only = true\n",
		"observe-only transport": "[[remote]]\nname = \"watch\"\ntransport = [\"ssh\", \"watch\"]\nobserve_only = true\n",
		"empty lane and ref":     "[queue]\nbase_ref = \"\"\n[[hook.rule]]\nmatch = [\" bun  test \", \"make\"]\nlane = \"\"\n",
		"base ref":               "[queue]\nbase_ref = \"origin/release/1.2\"\n",
		"no base ref":            "[queue]\nbase_ref = \"none\"\n",
		"binary path":            "[hook]\nbinary = \"/opt/heavy q/bin/heavyq\"\n",
		"cache env":              "[cache]\nenv = [\"CI\", \"NODE_ENV\", \"_X1\"]\n",
		"transport and git_url":  "[[remote]]\nname = \"box\"\ntransport = [\"ssh\", \"box\"]\ngit_url = \"box:.heavyq/git\"\n",
		"user at host alias":     "[[remote]]\nname = \"box.2\"\nssh = \"me@box-2\"\nwork_dir = \"cache/heavyq\"\nenv_path = \"~/go/bin:/usr/local/bin\"\n",
		"legacy locks locally":   "[local]\nlegacy_lock_files = [\"/tmp/build-1.lock\"]\n",
		"need_gh repos":          "[run]\nneed_gh_repos = [\"app\", \"infra.tools\"]\n",
		"list target for proofs": box + "[[remote]]\nname = \"spare\"\nssh = \"spare\"\n[proof]\nremote = \"box,local,spare\"\n",
		"examples/config.toml":   readFile(t, "examples/config.toml"),
	} {
		if err := loadConfig(t, doc); err != nil {
			t.Errorf("%s: heavyq refuses it (%v); this case belongs in the refused list", name, err)
			continue
		}
		if err := cfg.Validate(tomlDoc(t, doc)); err != nil {
			t.Errorf("%s: heavyq accepts it, config.schema.json refuses it: %v", name, err)
		}
	}
}

// [local] and [[remote]] are different tables: the keys of one are not offered in the other.
func TestUserConfigSchemaSplitsLocalAndRemoteKeys(t *testing.T) {
	inRepoRoot(t)
	cfg := compile(t, "config.schema.json")
	for _, doc := range []string{
		"[local]\nssh = \"box\"\n",
		"[local]\nwork_dir = \".heavyq\"\n",
		"[local]\nname = \"me\"\n",
	} {
		if err := cfg.Validate(tomlDoc(t, doc)); err == nil {
			t.Errorf("%q: a remote-only key is offered under [local]", doc)
		}
	}
}
