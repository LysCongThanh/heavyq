package pools

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// A pool exports HEAVYQ_<NAME> into its command, after the inherited
// environment, so the command and every heavyq it runs see the pool's value.
// A pool named after a variable heavyq reads would steer those nested runs:
// "history" moves their ledger, "flow" = off stops flow refresh, "test" turns on
// test switches. Every such name must be refused, by Build and by ParseNeeds.
func TestBuildRefusesNamesOfVariablesHeavyqReads(t *testing.T) {
	for _, name := range []string{
		"test", "history", "flow", "steer", "mentor", "notify", "stances",
		"mentor_seed", "notify_budget", "flow_gh", "with", "subdir", "base_ref", "base_sha",
		"env_path", "cache_dir", "job_id", "worktree", "test_fake_metrics",
		"lifeline", "cmd_env_b64", "job_quiet", "test_mentor_budget", "test_mentor_delay",
	} {
		if _, err := Build([]Spec{{Name: name, Size: 2}}); err == nil || !strings.Contains(err.Error(), "reserved") {
			t.Errorf("pool %q: Build = %v, want a reserved-name error", name, err)
		}
		if _, err := ParseNeeds(name); err == nil {
			t.Errorf("ParseNeeds(%q) accepted", name)
		}
	}
}

// examplePools are pool names the docs and tests use as examples; their
// HEAVYQ_<NAME> is a pool's variable, not one of heavyq's.
var examplePools = map[string]bool{"pg": true, "port_1": true, "port_2": true, "port_n": true, "browser": true, "one": true}

// heavyqVariable finds the variables named in a source file.
var heavyqVariable = regexp.MustCompile(`HEAVYQ_([A-Z0-9_]*[A-Z0-9])`)

// The reserved set is not a list someone remembers to update: every
// HEAVYQ_<NAME> written anywhere in the repository (code, tests, scripts, the
// man page, docs) is either reserved or one of the example pools above.
func TestReservedCoversEveryVariableInTheRepository(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("repository root not found from %s: %v", root, err)
	}
	found := map[string]string{} // pool name -> first file that names it
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if d.IsDir() {
			// Hidden directories (.git, editor and agent state) and plans hold no
			// heavyq source; changelog.d and CHANGELOG.md are history.
			if path != root && (strings.HasPrefix(name, ".") && name != ".github" || name == "plans" || name == "changelog.d") {
				return filepath.SkipDir
			}
			return nil
		}
		switch filepath.Ext(name) {
		case ".go", ".sh", ".md", ".toml", ".yml", ".yaml", ".tape":
		default:
			return nil
		}
		if name == "CHANGELOG.md" {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, m := range heavyqVariable.FindAllSubmatch(data, -1) {
			pool := strings.ToLower(string(m[1]))
			if _, ok := found[pool]; !ok {
				rel, _ := filepath.Rel(root, path)
				found[pool] = rel
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(found) < 20 {
		t.Fatalf("found only %d variables under %s; the scan is broken", len(found), root)
	}
	var missing []string
	for pool, file := range found {
		if examplePools[pool] {
			if !ValidName(pool) {
				t.Errorf("example pool %q is refused", pool)
			}
			continue
		}
		if ValidName(pool) {
			missing = append(missing, pool+" (HEAVYQ_"+strings.ToUpper(pool)+", "+file+")")
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Fatalf("pool names that would set a variable heavyq uses are accepted; add them to reservedNames:\n  %s",
			strings.Join(missing, "\n  "))
	}
}
