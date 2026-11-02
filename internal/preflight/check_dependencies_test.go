package preflight

import (
	"reflect"
	"strings"
	"testing"
)

func TestCheckDependencies(t *testing.T) {
	cases := []struct {
		name  string
		items []ItemFact
		want  []string
	}{
		{"nothing special", []ItemFact{item("a", nil), item("b", func(i *ItemFact) { i.After = []string{"a"} })}, nil},
		{"needs span", []ItemFact{item("ui", func(i *ItemFact) { i.Needs, i.NeedsInFile = "design sign-off", true })},
			[]string{"block:item ui needs a human: design sign-off"}},
		{"blocked by event", []ItemFact{item("ui", func(i *ItemFact) { i.State, i.Needs = "blocked", "a Stripe key" })},
			[]string{"block:a Stripe key"}},
		{"blocked finished item is not asked again", []ItemFact{item("ui", func(i *ItemFact) { i.State, i.Done, i.Needs = "proven", true, "x" })}, nil},
		{"after names a missing item", []ItemFact{item("b", func(i *ItemFact) { i.After = []string{"zzz"} })},
			[]string{`block:item b waits on "zzz"`}},
		{"after a finished item is fine", []ItemFact{
			item("a", func(i *ItemFact) { i.State, i.Done = "proven", true }),
			item("b", func(i *ItemFact) { i.After = []string{"a"} })}, nil},
		{"a loop", []ItemFact{
			item("a", func(i *ItemFact) { i.After = []string{"b"} }),
			item("b", func(i *ItemFact) { i.After = []string{"c"} }),
			item("c", func(i *ItemFact) { i.After = []string{"a"} })},
			[]string{"block:items wait on each other: a -> b -> c -> a"}},
		{"self loop", []ItemFact{item("a", func(i *ItemFact) { i.After = []string{"a"} })},
			[]string{"block:items wait on each other: a -> a"}},
		{"diamond is not a loop", []ItemFact{
			item("a", nil),
			item("b", func(i *ItemFact) { i.After = []string{"a"} }),
			item("c", func(i *ItemFact) { i.After = []string{"a"} }),
			item("d", func(i *ItemFact) { i.After = []string{"b", "c"} })}, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) { assertFindings(t, CheckDependencies(c.items), c.want) })
	}
}

func TestNeedsFixDependsOnWhereItIsWritten(t *testing.T) {
	fs := CheckDependencies([]ItemFact{
		item("a", func(i *ItemFact) { i.Needs, i.NeedsInFile = "x", true }),
		item("b", func(i *ItemFact) { i.State, i.Needs = "blocked", "y" })})
	if !strings.Contains(fs[0].Fix, "delete the `needs:` span at .heavyq/anchor.md:3") ||
		fs[1].Fix != "answer it, then `heavyq item claim b`" {
		t.Errorf("fixes: %+v", fs)
	}
}

func TestParseAfter(t *testing.T) {
	cases := map[string][]string{
		"- [ ] b: do it `proof: go test ./...` `after: a, c`": {"a", "c"},
		"- [ ] b: do it `after: a c`":                         {"a", "c"},
		"- [ ] b: `AFTER:a`":                                  {"a"},
		"- [ ] b: no spans":                                   nil,
		"- [ ] b: `after:`":                                   {},
		"- [ ] b: `proof: x` `needs: y`":                      nil,
		"- [ ] b: unterminated `after: a":                     nil,
	}
	for line, want := range cases {
		got := ParseAfter(line)
		if len(got) == 0 && len(want) == 0 {
			continue
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("ParseAfter(%q) = %v, want %v", line, got, want)
		}
	}
}
