package repoconfig

import (
	"reflect"
	"strings"
	"testing"
)

func TestLoadGateTools(t *testing.T) {
	r, err := Load(newRepo(t, "[[gate]]\nname = \"e2e\"\nrun = [\"bash scripts/e2e.sh\"]\nremote = \"auto\"\ntools = [\"envsubst\", \"initdb\"]\n"))
	if err != nil || r == nil {
		t.Fatal(r, err)
	}
	if g, _ := r.Gate("e2e"); !reflect.DeepEqual(g.Tools, []string{"envsubst", "initdb"}) {
		t.Fatalf("tools = %q", g.Tools)
	}
}

func TestGateToolsAreValidated(t *testing.T) {
	for name, tools := range map[string]string{
		"a path":        `tools = ["/usr/bin/envsubst"]`,
		"relative path": `tools = ["../bin/x"]`,
		"shell syntax":  `tools = ["x;y"]`,
		"a space":       `tools = ["a b"]`,
		"an option":     `tools = ["-rf"]`,
		"comma inside":  `tools = ["pg,psql"]`,
		"empty element": `tools = [""]`,
		"not a list":    `tools = "envsubst"`,
	} {
		body := "[[gate]]\nname = \"g\"\nrun = [\"true\"]\n" + tools + "\n"
		_, err := Load(newRepo(t, body))
		if err == nil || !strings.Contains(err.Error(), FileName) || !strings.Contains(err.Error(), "tools") {
			t.Errorf("%s: want an error naming the file and tools, got %v", name, err)
		}
	}
}
