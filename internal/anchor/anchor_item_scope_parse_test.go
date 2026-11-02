package anchor

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseReadsPathsAndAfter(t *testing.T) {
	f := Parse("a.md", []byte("# Goal: g\n\n"+
		"- [ ] flow: split it  `proof: gate:flow`  `paths: internal/flow/**, ./docs/flow.md`  `after: proof-scope plans/x/phase-1/2-y`\n"+
		"- [ ] plain: nothing declared\n"))
	if len(f.Warnings) != 0 {
		t.Fatalf("warnings: %v", f.Warnings)
	}
	it := f.Items[0]
	if want := []string{"internal/flow/**", "docs/flow.md"}; !reflect.DeepEqual(it.Paths, want) {
		t.Fatalf("paths = %q, want %q", it.Paths, want)
	}
	if want := []string{"proof-scope", "plans/x/phase-1/2-y"}; !reflect.DeepEqual(it.After, want) {
		t.Fatalf("after = %q, want %q", it.After, want)
	}
	if it.Text != "split it" || it.Proof.Value != "flow" {
		t.Fatalf("spans must leave the text and proof alone: %+v", it)
	}
	if p := f.Items[1]; p.Paths != nil || p.After != nil {
		t.Fatalf("undeclared must stay nil: %+v", p)
	}
}

func TestParseScopeSpansWarnAndDropBadEntries(t *testing.T) {
	f := Parse("a.md", []byte("# Goal: g\n"+
		"- [ ] a: x `paths: /etc/**, ../up/**, ok/**, [bad` `paths: second/**` `after: Bad_ID, good-1` `after: other`\n"+
		"- [ ] b: y `paths:` `after: ,`\n"))
	a := f.Items[0]
	if want := []string{"ok/**"}; !reflect.DeepEqual(a.Paths, want) {
		t.Fatalf("paths = %q", a.Paths)
	}
	if want := []string{"good-1"}; !reflect.DeepEqual(a.After, want) {
		t.Fatalf("after = %q", a.After)
	}
	if b := f.Items[1]; b.Paths != nil || b.After != nil {
		t.Fatalf("empty spans must declare nothing: %+v", b)
	}
	var all []string
	for _, w := range f.Warnings {
		all = append(all, w.Msg)
	}
	got := strings.Join(all, "\n")
	for _, want := range []string{`"/etc/**"`, `"../up/**"`, `"[bad" is malformed`, "more than one `paths:`",
		`"Bad_ID" is not an item id`, "more than one `after:`", "empty `paths:` ignored", "empty `after:` ignored"} {
		if !strings.Contains(got, want) {
			t.Errorf("no warning containing %q in:\n%s", want, got)
		}
	}
}

func TestParseExtraReadsScope(t *testing.T) {
	items, _ := ParseExtra("plans/p/phase-1.md", []byte("- [ ] do it `paths: src/**` `after: other`\n"))
	if len(items) != 1 || !reflect.DeepEqual(items[0].Paths, []string{"src/**"}) || !reflect.DeepEqual(items[0].After, []string{"other"}) {
		t.Fatalf("items = %+v", items)
	}
}
