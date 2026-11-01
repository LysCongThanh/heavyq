package runident

import (
	"strings"
	"testing"
)

func envOf(m map[string]string) func(string) (string, bool) {
	return func(k string) (string, bool) { v, ok := m[k]; return v, ok }
}

func base() Input {
	return Input{Tree: "t1", Argv: []string{"sh", "-c", "go test"}, EnvNames: []string{"A", "B"}, Getenv: envOf(nil)}
}

func TestIDIsHexSha256AndStable(t *testing.T) {
	a, b := ID(base()), ID(base())
	if a != b || len(a) != 64 || strings.Trim(a, "0123456789abcdef") != "" {
		t.Fatalf("ID = %q, %q", a, b)
	}
}

func TestIDChangesWithEveryComponent(t *testing.T) {
	ref := ID(base())
	cases := map[string]func(*Input){
		"tree":       func(i *Input) { i.Tree = "t2" },
		"argv":       func(i *Input) { i.Argv = []string{"sh", "-c", "go vet"} },
		"argv split": func(i *Input) { i.Argv = []string{"sh", "-c go", "test"} },
		"subdir":     func(i *Input) { i.Subdir = "sub" },
		"env set":    func(i *Input) { i.Getenv = envOf(map[string]string{"A": "1"}) },
		"env empty":  func(i *Input) { i.Getenv = envOf(map[string]string{"A": ""}) },
		"base":       func(i *Input) { i.BaseSHA = "abc" },
		"sibling":    func(i *Input) { i.Siblings = []string{"x@1"} },
	}
	seen := map[string]string{ref: "reference"}
	for name, mod := range cases {
		in := base()
		mod(&in)
		id := ID(in)
		if prev, dup := seen[id]; dup {
			t.Errorf("%s gives the same id as %s", name, prev)
		}
		seen[id] = name
	}
}

func TestIDEnvIgnoresNonAllowlistedAndNameOrder(t *testing.T) {
	a := base()
	a.Getenv = envOf(map[string]string{"OTHER": "x"})
	if ID(a) != ID(base()) {
		t.Error("a variable outside the allowlist changed the id")
	}
	b, c := base(), base()
	b.EnvNames, c.EnvNames = []string{"A", "B", "A"}, []string{"B", "A"}
	if ID(b) != ID(c) {
		t.Error("allowlist order or duplicates changed the id")
	}
}

func TestUnknownTreeHasNoIdentity(t *testing.T) {
	in := base()
	in.Tree = ""
	if ID(in) != "" {
		t.Fatal("unknown tree must have no id")
	}
}
