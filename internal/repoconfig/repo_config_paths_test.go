package repoconfig

import (
	"reflect"
	"strings"
	"testing"
)

// A gate may declare the files its proof identity is made of.
func TestLoadGatePaths(t *testing.T) {
	r, err := Load(newRepo(t, `
[[gate]]
name = "go"
run = ["go test ./..."]
paths = ["**/*.go", "go.mod", "go.sum", "testdata/**"]

[[gate]]
name = "all"
run = ["true"]
`))
	if err != nil || r == nil {
		t.Fatal(r, err)
	}
	g, _ := r.Gate("go")
	if want := []string{"**/*.go", "go.mod", "go.sum", "testdata/**"}; !reflect.DeepEqual(g.Paths, want) {
		t.Fatalf("paths = %v, want %v", g.Paths, want)
	}
	if all, _ := r.Gate("all"); len(all.Paths) != 0 {
		t.Fatalf("a gate without paths is the whole tree, got %v", all.Paths)
	}
}

// Unsafe or unknown shapes of `paths` are errors that name the entry; nothing is guessed.
func TestGatePathsAreValidated(t *testing.T) {
	for name, body := range map[string]string{
		"not a list":     `paths = "**/*.go"`,
		"empty entry":    `paths = [""]`,
		"absolute":       `paths = ["/etc/passwd"]`,
		"dot dot":        `paths = ["../x"]`,
		"dot":            `paths = ["./a"]`,
		"empty element":  `paths = ["a//b"]`,
		"char class":     `paths = ["a/[bc].go"]`,
		"escape":         `paths = ["a\\*"]`,
		"trailing slash": `paths = ["docs/"]`,
		"glued stars":    `paths = ["a**b"]`,
		"magic":          `paths = [":(exclude)x"]`,
		"negation":       `paths = ["!x"]`,
		"twice":          `paths = ["a", "a"]`,
		"too many":       "paths = [" + strings.Repeat(`"a",`, 65) + `"b"]`,
		"too long":       `paths = ["` + strings.Repeat("a", 201) + `"]`,
	} {
		_, err := Load(newRepo(t, "[[gate]]\nname = \"g\"\nrun = [\"true\"]\n"+body+"\n"))
		if err == nil {
			t.Errorf("%s: accepted", name)
		} else if !strings.Contains(err.Error(), "paths") {
			t.Errorf("%s: the error does not name the key: %v", name, err)
		}
	}
}
