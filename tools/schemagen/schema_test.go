package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/LysCongThanh/heavyq/internal/fleet"
	"github.com/LysCongThanh/heavyq/internal/snapshot"
)

// The tests run in tools/schemagen; the repository root is two levels up. build() reads Go comments
// relative to the root, so they run from there.
func inRepoRoot(t *testing.T) {
	t.Helper()
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir("../.."); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(old) })
}

// The committed schemas are what the generator produces now: the same check CI runs with git diff.
func TestCommittedSchemasAreCurrent(t *testing.T) {
	inRepoRoot(t)
	for _, f := range schemaFiles() {
		want, err := build(f)
		if err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(filepath.Join("schema", f.File))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) {
			t.Errorf("schema/%s is stale: run `cd tools/schemagen && go run .` and commit the result", f.File)
		}
	}
}

func compile(t *testing.T, file string) *jsonschema.Schema {
	t.Helper()
	c := jsonschema.NewCompiler()
	c.DefaultDraft(jsonschema.Draft7)
	s, err := c.Compile(filepath.Join("schema", file))
	if err != nil {
		t.Fatalf("%s: %v", file, err)
	}
	return s
}

// tomlDoc reads TOML text into the generic JSON shape a schema validates.
func tomlDoc(t *testing.T, text string) any {
	t.Helper()
	var v map[string]any
	if _, err := toml.Decode(text, &v); err != nil {
		t.Fatalf("bad TOML in the test: %v", err)
	}
	return viaJSON(t, v)
}

func viaJSON(t *testing.T, v any) any {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	out, err := jsonschema.UnmarshalJSON(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// The files shipped in the repository must satisfy the schemas that are published for them.
func TestShippedFilesValidate(t *testing.T) {
	inRepoRoot(t)
	for _, c := range []struct{ schema, file string }{
		{"heavyq.schema.json", "heavyq.toml"},
		{"config.schema.json", "examples/config.toml"},
	} {
		if err := compile(t, c.schema).Validate(tomlDoc(t, readFile(t, c.file))); err != nil {
			t.Errorf("%s does not satisfy %s: %v", c.file, c.schema, err)
		}
	}
}

// What the validators refuse, the schemas refuse too; and a full valid file passes.
func TestSchemasRefuseWhatHeavyqRefuses(t *testing.T) {
	inRepoRoot(t)
	repo, cfg := compile(t, "heavyq.schema.json"), compile(t, "config.schema.json")
	for name, doc := range map[string]string{
		"gate":             "[[gate]]\nname = \"ready\"\nrun = [\"go test ./...\"]\nprio = 2\nremote = \"auto\"\nneeds = [\"pg\", \"port:2\"]\n[[pool]]\nname = \"port\"\nvalues = \"4100-4199\"\n[[pool]]\nname = \"pg\"\nsize = 3\n[anchor]\nfile = \"x.md\"\n",
		"empty document":   "",
		"pool with a list": "[[pool]]\nname = \"db\"\nvalues = [\"a\", \"b\", 3]\n",
	} {
		if err := repo.Validate(tomlDoc(t, doc)); err != nil {
			t.Errorf("heavyq.toml %q should be valid: %v", name, err)
		}
	}
	for name, doc := range map[string]string{
		"upper-case gate name": "[[gate]]\nname = \"Ready\"\nrun = [\"x\"]\n",
		"gate without run":     "[[gate]]\nname = \"ready\"\n",
		"empty run":            "[[gate]]\nname = \"ready\"\nrun = []\n",
		"blank command":        "[[gate]]\nname = \"ready\"\nrun = [\"  \"]\n",
		"prio out of range":    "[[gate]]\nname = \"ready\"\nrun = [\"x\"]\nprio = 10\n",
		"unknown gate key":     "[[gate]]\nname = \"ready\"\nrun = [\"x\"]\nlight_ = true\n",
		"pool without values":  "[[pool]]\nname = \"p\"\n",
		"unknown top-level":    "[gates]\n",
	} {
		if err := repo.Validate(tomlDoc(t, doc)); err == nil {
			t.Errorf("heavyq.toml %q should be refused", name)
		}
	}
	for name, doc := range map[string]string{
		"full":  "[queue]\npoll = \"5s\"\nresource_gate = false\n[local]\nheavy_slots = 4\n[[remote]]\nname = \"box\"\nssh = \"box\"\n[hook]\npermission = \"allow\"\n[[hook.rule]]\nmatch = [\"bun test\"]\n[cache]\nttl = \"1h30m\"\n",
		"fleet": "[[remote]]\nname = \"watch\"\nssh = \"watch\"\nobserve_only = true\n[[foreign]]\nname = \"bashq\"\nhistory = \"/q/h.tsv\"\ncolumns = [\"end_epoch\", \"machine\", \"exit\"]\ninfra_exits = [97]\nslot_dirs = { watch = \"q/slots\" }\nslot_globs = { local = \"/tmp/q-[0-9]*.lock\" }\n",
	} {
		if err := cfg.Validate(tomlDoc(t, doc)); err != nil {
			t.Errorf("config %q should be valid: %v", name, err)
		}
	}
	for name, doc := range map[string]string{
		"duration without unit":  "[queue]\npoll = \"5\"\n",
		"unknown section":        "[queu]\npoll = \"5s\"\n",
		"unknown key":            "[queue]\npol = \"5s\"\n",
		"bad permission":         "[hook]\npermission = \"yes\"\n",
		"rule without match":     "[[hook.rule]]\nlane = \"x\"\n",
		"foreign unknown column": "[[foreign]]\nname = \"bashq\"\nhistory = \"/q/h.tsv\"\ncolumns = [\"color\"]\n",
		"foreign named heavyq":   "[[foreign]]\nname = \"heavyq\"\ntickets = \"/q\"\n",
		"foreign with nothing":   "[[foreign]]\nname = \"bashq\"\n",
		"foreign exit 0":         "[[foreign]]\nname = \"bashq\"\ntickets = \"/q\"\ninfra_exits = [0]\n",
	} {
		if err := cfg.Validate(tomlDoc(t, doc)); err == nil {
			t.Errorf("config %q should be refused", name)
		}
	}
}

// A fleet report as `heavyq fleet --json` prints it satisfies fleet.schema.json.
func TestFleetSchemaAcceptsAReport(t *testing.T) {
	inRepoRoot(t)
	now := time.Date(2026, 11, 12, 10, 0, 0, 0, time.UTC)
	r := fleet.Assemble(fleet.Inputs{Now: now, Since: now.Add(-24 * time.Hour),
		Machines: []fleet.MachineInput{
			{Name: "local", Role: fleet.RoleLocal, HeavyqSlots: 2, HeavyqTotal: 3, Probe: &fleet.Probe{Cores: 8, Load1: 20, FreeMB: 100, TotalMB: 8000,
				Procs: []fleet.Proc{{Pid: 2, PPid: 1, CPU: 100, RSSKB: 2048, Comm: "node"}}}},
			{Name: "gone", Role: fleet.RoleObserveOnly, Err: errors.New("timed out")},
			{Name: "slow", Role: fleet.RoleObserveOnly, Probe: &fleet.Probe{Cores: 18, Load1: 70, FreeMB: 80000, TotalMB: 96000,
				Partial: "no top consumers and attribution (timed out after 30s)"}},
			// Probes cut before their slots: on "cut" heavyq's 4 configured slots are counted, with a note; on
			// "cut-unknown" nothing is configured, so there is no cap and worker_cap_unknown says why.
			{Name: "cut", Role: fleet.RoleRemote, HeavyqSlots: 4, HeavyqTotal: 4, Probe: &fleet.Probe{Cores: 16, Load1: 3, FreeMB: 9000, TotalMB: 16000,
				SlotsUnread: []string{"heavyq"}, Partial: "no slot states, top consumers and attribution (timed out after 30s)"}},
			{Name: "cut-unknown", Role: fleet.RoleObserveOnly, Probe: &fleet.Probe{Cores: 16, Load1: 3, FreeMB: 9000, TotalMB: 16000,
				SlotsUnread: []string{"heavyq"}, Partial: "no slot states, top consumers and attribution (timed out after 30s)"}},
		},
		Queues: []fleet.QueueInput{{Name: "heavyq", Kind: fleet.KindHeavyq, Runs: []fleet.Run{{Machine: "local", WaitS: 1, RunS: 2, Exit: 255, Class: fleet.ClassInfra}},
			Tickets: []fleet.Ticket{{Lane: "api", Prio: 3, Since: now.Add(-time.Hour)}}}},
	})
	schema := compile(t, "fleet.schema.json")
	doc := viaJSON(t, r)
	if err := schema.Validate(doc); err != nil {
		t.Fatalf("report rejected: %v", err)
	}
	machines := doc.(map[string]any)["machines"].([]any)
	cut, unknown := machines[3].(map[string]any), machines[4].(map[string]any)
	if cut["worker_cap"] == nil || unknown["worker_cap"] != nil || unknown["worker_cap_unknown"] == nil {
		t.Fatalf("cut probes: %v\n%v", cut, unknown)
	}
	unknown["worker_cap_unknown"] = true
	if err := schema.Validate(doc); err == nil {
		t.Error("worker_cap_unknown is text: a boolean should be refused")
	}
	delete(unknown, "worker_cap_unknown")
	slow := machines[2].(map[string]any)
	if slow["partial"] == nil || slow["reachable"] != true {
		t.Fatalf("a partial machine: %v", slow)
	}
	slow["partial"] = 5
	if err := schema.Validate(doc); err == nil {
		t.Error("partial is text: a number should be refused")
	}
	slow["partial"] = "no command lines of the busiest"
	delete(doc.(map[string]any), "queues")
	if err := schema.Validate(doc); err == nil {
		t.Error("a report without queues should be refused")
	}
}

// A snapshot as heavyq prints it (empty arrays, never null) satisfies status.schema.json, and a reader
// can ignore fields added later.
func TestStatusSchemaAcceptsASnapshot(t *testing.T) {
	inRepoRoot(t)
	s := snapshot.Snapshot{
		Schema: snapshot.Schema, Version: "1.2.3", Host: "h",
		CPU:      snapshot.CPU{Lanes: []snapshot.Usage{}, Outside: []snapshot.Usage{}},
		Machines: []snapshot.Machine{}, Running: []snapshot.Running{}, Waiting: []snapshot.Waiting{},
		Coalesced: []snapshot.Group{}, Story: []snapshot.Sentence{}, Gates: []snapshot.LaneGate{},
		Exceptions: []snapshot.Exception{}, Pools: []snapshot.Pool{}, Items: []snapshot.LaneItems{},
	}
	doc := viaJSON(t, s)
	schema := compile(t, "status.schema.json")
	if err := schema.Validate(doc); err != nil {
		t.Fatalf("snapshot rejected: %v", err)
	}
	doc.(map[string]any)["added_later"] = true
	if err := schema.Validate(doc); err != nil {
		t.Errorf("an added field must be ignored: %v", err)
	}
	delete(doc.(map[string]any), "machines")
	if err := schema.Validate(doc); err == nil {
		t.Error("a snapshot without machines should be refused")
	}
}
