package pathscope

import "testing"

func TestIntersects(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"internal/flow/**", "internal/flow/a.go", true},
		{"internal/flow/**", "internal/recap/**", false},
		{"internal/flow/**", "internal/**", true},
		{"internal/flow", "internal/flow/a.go", true}, // a literal may be a directory
		{"internal/flow/", "internal/flow/x/y.go", true},
		{"internal/flow/a.go", "internal/flow/b.go", false},
		{"internal/flow/a.go", "internal/flow/a.go", true},
		{"internal/*/a.go", "internal/flow/a.go", true},
		{"internal/*/a.go", "internal/flow/b.go", false},
		{"**/a.go", "internal/flow/a.go", true},
		{"**/a.go", "internal/flow/b.go", false},
		{"**/*_test.go", "internal/cli/**", true},
		{"**/*.md", "**/*.go", false},
		{"*.go", "internal/a.go", false},
		{"*.go", "a.go", true},
		{"docs/**", "internal/**", false},
		{"./docs/a.md", "docs/a.md", true},
		{"a/**/z", "a/b/c/z", true},
		{"a/**/z", "a/b/c/y", true}, // z could be a directory holding y
		{"a/**/z.go", "a/b/c/y.go", false},
		{".github", ".github/ci.yml", true},
		{"cmd/*_a.go", "cmd/x_b.go", false}, // tails differ
		{"cmd/a_*.go", "cmd/b_*.go", false}, // heads differ
		{"cmd/a*.go", "cmd/*b.go", true},    // cannot tell: overlap
		{"[ab].go", "a.go", true},
		{"[ab].go", "c.go", false},
		{"[", "x", true}, // malformed: conservative
		{"", "internal/flow", true},
	}
	for _, c := range cases {
		if got := Intersects(c.a, c.b); got != c.want {
			t.Errorf("Intersects(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
		if got := Intersects(c.b, c.a); got != c.want {
			t.Errorf("Intersects(%q, %q) = %v, want %v (swapped)", c.b, c.a, got, c.want)
		}
	}
}

func TestOverlapNamesThePair(t *testing.T) {
	a, b, ok := Overlap([]string{"docs/**", "internal/flow/**"}, []string{"cmd/**", "internal/flow/x.go"})
	if !ok || a != "internal/flow/**" || b != "internal/flow/x.go" {
		t.Fatalf("Overlap = %q, %q, %v", a, b, ok)
	}
	if _, _, ok := Overlap([]string{"docs/**"}, []string{"cmd/**"}); ok {
		t.Fatal("disjoint globs overlapped")
	}
	if _, _, ok := Overlap(nil, []string{"cmd/**"}); ok {
		t.Fatal("undeclared paths must overlap nothing")
	}
}

func TestIntersectsManyStarsStaysFast(t *testing.T) {
	a := "**/**/**/**/**/**/**/**/**/**/x.go"
	b := "a/**/b/**/c/**/d/**/e/**/y.go"
	if Intersects(a, b) {
		t.Fatal("x.go and y.go tails cannot meet")
	}
}
