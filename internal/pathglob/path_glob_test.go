package pathglob

import "testing"

func TestMatch(t *testing.T) {
	cases := []struct {
		pattern, name string
		want          bool
	}{
		{"**/*.go", "main.go", true},
		{"**/*.go", "internal/cli/run.go", true},
		{"**/*.go", "README.md", false},
		{"**/*.go", "docs/go", false},
		{"*.go", "main.go", true},
		{"*.go", "internal/main.go", false},
		{"go.mod", "go.mod", true},
		{"go.mod", "sub/go.mod", false},
		{"docs/**", "docs/gates.md", true},
		{"docs/**", "docs/a/b.md", true},
		{"docs/**", "docs", false},
		{"a/**/b.txt", "a/b.txt", true},
		{"a/**/b.txt", "a/x/y/b.txt", true},
		{"a/**/b.txt", "b.txt", false},
		{"**", "any/thing", true},
		{"**/testdata/**", "internal/x/testdata/f.json", true},
		{"[ab].go", "a.go", true},
		{"[ab].go", "c.go", false},
		{"Makefile", "makefile", false}, // no case folding
	}
	for _, c := range cases {
		m, err := Compile([]string{c.pattern})
		if err != nil {
			t.Fatalf("Compile(%q): %v", c.pattern, err)
		}
		if got := m.Match(c.name); got != c.want {
			t.Errorf("%q vs %q = %v, want %v", c.pattern, c.name, got, c.want)
		}
	}
}

func TestMatchAnyOfSeveralAndZeroValue(t *testing.T) {
	m, err := Compile([]string{"**/*.go", "go.mod"})
	if err != nil {
		t.Fatal(err)
	}
	if !m.Match("go.mod") || !m.Match("a/b.go") || m.Match("a/b.md") {
		t.Error("any pattern of the list may match")
	}
	if (Matcher{}).Match("go.mod") {
		t.Error("the zero Matcher matches nothing")
	}
}

func TestValidateRefuses(t *testing.T) {
	for _, p := range []string{"", "  ", "/abs", "dir/", "a//b", "../x", "a/./b", `a\b`, "a**/b", "**.go", "[", "a/[b"} {
		if err := Validate(p); err == nil {
			t.Errorf("Validate(%q) accepted it", p)
		}
	}
	for _, p := range []string{"**/*.go", "go.mod", "docs/**", "a/**/b"} {
		if err := Validate(p); err != nil {
			t.Errorf("Validate(%q) = %v", p, err)
		}
	}
	if _, err := Compile([]string{"ok", "../bad"}); err == nil {
		t.Error("Compile must refuse a list with a bad pattern")
	}
}
