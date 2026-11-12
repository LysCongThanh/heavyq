package main

import (
	"encoding/json"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/LysCongThanh/heavyq/internal/anchor"
)

// The schemas, the docs and examples/config.toml describe the same keys; these tests keep them from drifting
// apart when the code moves (the generated schema follows the Go types, the prose does not).

// generated decodes one committed schema into the generic JSON shape.
func generated(t *testing.T, file string) map[string]any {
	t.Helper()
	var root map[string]any
	if err := json.Unmarshal([]byte(readFile(t, "schema/"+file)), &root); err != nil {
		t.Fatalf("%s: %v", file, err)
	}
	return root
}

// table returns the object schema a top-level key leads to (through $ref, or the items of an array of
// tables), or nil when the key is a plain value.
func table(root, s map[string]any) map[string]any {
	if items, ok := s["items"].(map[string]any); ok && s["type"] == "array" {
		s = items
	}
	if ref, ok := s["$ref"].(string); ok {
		s = root["definitions"].(map[string]any)[strings.TrimPrefix(ref, "#/definitions/")].(map[string]any)
	}
	if _, ok := s["properties"]; !ok {
		return nil
	}
	return s
}

// schemaKeys lists every leaf key of a TOML schema as "section.key", sections as TOML spells them
// ("hook.rule" for [[hook.rule]]).
func schemaKeys(root, s map[string]any, section string, out map[string][]string) {
	for key, p := range s["properties"].(map[string]any) {
		name := key
		if section != "" {
			name = section + "." + key
		}
		if sub := table(root, p.(map[string]any)); sub != nil {
			schemaKeys(root, sub, name, out)
			continue
		}
		out[section] = append(out[section], key)
	}
}

var (
	exampleHeader = regexp.MustCompile(`^\[\[?\s*([a-z_.]+)\s*\]\]?`)
	exampleKey    = regexp.MustCompile(`^\s*(?:#\s*)?([a-z_]+)\s*=`)
)

// exampleKeys lists the keys examples/config.toml shows per section, commented-out examples included.
func exampleKeys(text string) map[string]map[string]bool {
	out := map[string]map[string]bool{}
	section := ""
	for _, line := range strings.Split(text, "\n") {
		if m := exampleHeader.FindStringSubmatch(line); m != nil {
			section = m[1]
			continue
		}
		if m := exampleKey.FindStringSubmatch(line); m != nil {
			if out[section] == nil {
				out[section] = map[string]bool{}
			}
			out[section][m[1]] = true
		}
	}
	return out
}

// docs/configuration.md says examples/config.toml has every key; every key the user config schema knows is
// there, in its section, set or as a commented example.
func TestExampleConfigShowsEveryKey(t *testing.T) {
	inRepoRoot(t)
	root := generated(t, "config.schema.json")
	want := map[string][]string{}
	schemaKeys(root, root, "", want)
	have := exampleKeys(readFile(t, "examples/config.toml"))
	var missing []string
	for section, keys := range want {
		for _, k := range keys {
			if !have[section][k] {
				missing = append(missing, section+"."+k)
			}
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("examples/config.toml does not show %v", missing)
	}
}

// A placement target may be an ordered list of machines; every description of a remote key says so.
func TestRemoteDescriptionsNameTheListForm(t *testing.T) {
	inRepoRoot(t)
	repo, cfg := generated(t, "heavyq.schema.json"), generated(t, "config.schema.json")
	for name, s := range map[string]map[string]any{
		"heavyq.toml [[gate]] remote": table(repo, repo["properties"].(map[string]any)["gate"].(map[string]any)),
		"heavyq.toml [proof] remote":  table(repo, repo["properties"].(map[string]any)["proof"].(map[string]any)),
		"config [proof] remote":       table(cfg, cfg["properties"].(map[string]any)["proof"].(map[string]any)),
	} {
		desc, _ := s["properties"].(map[string]any)["remote"].(map[string]any)["description"].(string)
		if !strings.Contains(desc, `"a,b,c"`) {
			t.Errorf("%s: the description does not mention the ordered list \"a,b,c\": %q", name, desc)
		}
	}
}

// The schema's default of [anchor] file is the one the loader uses.
func TestAnchorFileDefaultIsTheLoaders(t *testing.T) {
	inRepoRoot(t)
	repo := generated(t, "heavyq.schema.json")
	file := table(repo, repo["properties"].(map[string]any)["anchor"].(map[string]any))["properties"].(map[string]any)["file"].(map[string]any)
	if file["default"] != anchor.DefaultFile {
		t.Errorf("anchor.file default is %v in the schema, the loader uses %q", file["default"], anchor.DefaultFile)
	}
}

var gatesTableRow = regexp.MustCompile("(?m)^\\| `([a-z_]+)\\.([a-z_]+)` \\| [^|]+ \\| ([^|]+) \\|")

// The key table of docs/gates.md shows the default the schema carries, for every key that has one.
func TestGatesDocDefaultsMatchTheSchema(t *testing.T) {
	inRepoRoot(t)
	repo := generated(t, "heavyq.schema.json")
	props := repo["properties"].(map[string]any)
	rows := gatesTableRow.FindAllStringSubmatch(readFile(t, "docs/gates.md"), -1)
	if len(rows) < 10 {
		t.Fatalf("found %d key rows in docs/gates.md; the table format changed, update this test", len(rows))
	}
	for _, row := range rows {
		section, key, cell := row[1], row[2], row[3]
		p, ok := props[section].(map[string]any)
		if !ok {
			t.Errorf("docs/gates.md documents %s.%s, the schema has no [%s]", section, key, section)
			continue
		}
		s := table(repo, p)
		if s == nil {
			continue
		}
		prop, ok := s["properties"].(map[string]any)[key].(map[string]any)
		if !ok {
			t.Errorf("docs/gates.md documents %s.%s, the schema has no such key", section, key)
			continue
		}
		def, ok := prop["default"]
		if !ok {
			continue
		}
		b, _ := json.Marshal(def)
		if !strings.Contains(strings.ReplaceAll(cell, "`", ""), string(b)) {
			t.Errorf("docs/gates.md: %s.%s default is %q, the schema says %s", section, key, strings.TrimSpace(cell), b)
		}
	}
}
