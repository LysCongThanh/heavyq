package gatepaths

import (
	"reflect"
	"strings"
	"testing"
)

func TestValidAcceptsTheDocumentedShapes(t *testing.T) {
	for _, p := range []string{"**/*.go", "go.mod", "docs/**", "src/**/*_test.go", "pkg", "a/?.txt", "a b/c.md", "ünï/*.go", "*.go", ".github/workflows/*.yml"} {
		if err := Valid(p); err != nil {
			t.Errorf("%q refused: %v", p, err)
		}
	}
}

func TestValidRefusesEverythingElse(t *testing.T) {
	for p, why := range map[string]string{
		"":                       "empty",
		"/etc":                   "absolute",
		"../x":                   "..",
		"a/../b":                 "..",
		"./a":                    ".",
		"a//b":                   "empty component",
		"docs/":                  "trailing slash",
		"a[bc]":                  "class",
		`a\*`:                    "escape",
		"a**b":                   "glued stars",
		"**x/y":                  "glued stars",
		":(exclude)x":            "magic",
		"!x":                     "negation",
		"a\x00b":                 "NUL",
		"a\nb":                   "control",
		strings.Repeat("a", 201): "too long",
	} {
		if Valid(p) == nil {
			t.Errorf("%q accepted (%s)", p, why)
		}
	}
}

func TestValidListLimitsAndDuplicates(t *testing.T) {
	if err := ValidList([]string{"a", "b"}); err != nil {
		t.Fatal(err)
	}
	if ValidList([]string{"a", "a"}) == nil {
		t.Error("a duplicate was accepted")
	}
	many := make([]string, MaxEntries+1)
	for i := range many {
		many[i] = strings.Repeat("x", i+1)
	}
	if ValidList(many) == nil {
		t.Error("too many entries were accepted")
	}
	if err := ValidList(many[:MaxEntries]); err != nil {
		t.Errorf("exactly the limit: %v", err)
	}
}

func TestCanonicalSortsAndDropsRepeatsWithoutTouchingTheInput(t *testing.T) {
	in := []string{"b", "a", "b"}
	if got := Canonical(in); !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Fatalf("got %v", got)
	}
	if !reflect.DeepEqual(in, []string{"b", "a", "b"}) {
		t.Fatalf("the input changed: %v", in)
	}
}
