package anchor

import (
	"fmt"
	"strings"
	"testing"
)

const planBefore = `# Plan

- [ ] write parser
- [x] write rewriter  ` + "`proof: go test ./internal/anchor/...`" + `
- [ ] write docs
`

func idsByText(items []Item) map[string]string {
	m := map[string]string{}
	for _, it := range items {
		m[it.Text] = it.ID
	}
	return m
}

// Ids depend on the text and the ordinal among same-text items, never on line
// numbers: unrelated lines above, between or below leave them unchanged.
func TestExtraIDsStableWhenUnrelatedLinesChange(t *testing.T) {
	base, _ := ParseExtra("plans/p/phase-01.md", []byte(planBefore))
	if len(base) != 3 {
		t.Fatalf("items = %+v", base)
	}
	variants := map[string]string{
		"prose above":    "intro paragraph\n\n" + planBefore,
		"heading above":  "## Added heading\n" + planBefore,
		"other checkbox": "- [ ] something unrelated\n" + planBefore,
		"blank lines":    strings.ReplaceAll(planBefore, "\n- [", "\n\n- ["),
		"crlf":           strings.ReplaceAll(planBefore, "\n", "\r\n"),
		"appended":       planBefore + "- [ ] appended\n",
	}
	want := idsByText(base)
	for name, text := range variants {
		items, _ := ParseExtra("plans/p/phase-01.md", []byte(text))
		got := idsByText(items)
		for txt, id := range want {
			if got[txt] != id {
				t.Errorf("%s: id of %q = %q, want %q", name, txt, got[txt], id)
			}
		}
	}
}

func TestExtraIDsChangeWithTextAndPath(t *testing.T) {
	a, _ := ParseExtra("plans/p/phase-01.md", []byte("- [ ] one\n"))
	b, _ := ParseExtra("plans/p/phase-01.md", []byte("- [ ] one more\n"))
	c, _ := ParseExtra("plans/q/phase-01.md", []byte("- [ ] one\n"))
	if a[0].ID == b[0].ID || a[0].ID == c[0].ID {
		t.Fatalf("ids should differ: %q %q %q", a[0].ID, b[0].ID, c[0].ID)
	}
}

func TestExtraIDsSameTextGetOrdinals(t *testing.T) {
	items, _ := ParseExtra("p.md", []byte("- [ ] same\n- [ ] other\n- [ ] same\n- [ ] same\n"))
	want := []string{"p/1-same", "p/1-other", "p/2-same", "p/3-same"}
	for i, it := range items {
		if it.ID != want[i] {
			t.Errorf("item %d id = %q, want %q", i, it.ID, want[i])
		}
	}
}

func TestExtraItemsAreReadOnlyWithProofAndSource(t *testing.T) {
	items, warns := ParseExtra("plans/p/phase-01.md", []byte(planBefore))
	if len(warns) != 0 {
		t.Fatalf("warnings: %v", warns)
	}
	it := items[1]
	if !it.ReadOnly || !it.Checked || it.Source != "plans/p/phase-01.md" || it.Line != 4 {
		t.Fatalf("item = %+v", it)
	}
	if it.Proof != (Proof{Kind: ProofCmd, Value: "go test ./internal/anchor/..."}) || it.Text != "write rewriter" {
		t.Fatalf("proof/text = %+v", it)
	}
}

func TestExtraCapsItems(t *testing.T) {
	var b strings.Builder
	for i := 0; i < MaxItems+50; i++ {
		fmt.Fprintf(&b, "- [ ] task %d\n", i)
	}
	items, warns := ParseExtra("big.md", []byte(b.String()))
	if len(items) != MaxItems || len(warns) != 1 || !strings.Contains(warns[0].Msg, "2000") {
		t.Fatalf("items=%d warns=%v", len(items), warns)
	}
}

func TestSlugs(t *testing.T) {
	for in, want := range map[string]string{
		"Hello, World!":         "hello-world",
		"  --a__b--  ":          "a-b",
		"":                      "",
		"日本語":                   "",
		strings.Repeat("a", 80): strings.Repeat("a", maxTextSlug),
	} {
		if got := slug(in, maxTextSlug); got != want {
			t.Errorf("slug(%q) = %q, want %q", in, got, want)
		}
	}
	if a, b := textSlug("日本語"), textSlug("中文"); a == b || a == "" {
		t.Errorf("non-ASCII text slugs must differ: %q %q", a, b)
	}
	if got := textSlug("ascii"); got != "ascii" {
		t.Errorf("textSlug(ascii) = %q", got)
	}
	if got := fileSlug("plans/261101-x/phase-03-y.md"); got != "plans-261101-x-phase-03-y" {
		t.Errorf("fileSlug = %q", got)
	}
	long := fileSlug("plans/" + strings.Repeat("segment/", 20) + "file.md")
	if len(long) > maxFileSlug || long == fileSlug("plans/"+strings.Repeat("segment/", 20)+"other.md") {
		t.Errorf("long fileSlug = %q", long)
	}
}

func TestUniqueIDKeepsLengthLimit(t *testing.T) {
	id := strings.Repeat("a", MaxIDLen)
	used := map[string]bool{}
	uniqueID(used, id)
	got, dup := uniqueID(used, id)
	if !dup || len(got) > MaxIDLen || got == id {
		t.Fatalf("got %q dup=%v", got, dup)
	}
}

func TestIDPrefixRules(t *testing.T) {
	for _, tc := range []struct {
		in, id, rest string
		ok           bool
	}{
		{"a: x", "a", "x", true},
		{"a.b_c-d: x", "a.b_c-d", "x", true},
		{"**a**: x", "a", "x", true},
		{"**a:** x", "a", "x", true},
		{"_a_: x", "a", "x", true},
		{"`a`: x", "a", "x", true},
		{"a:", "a", "", true},
		{"a:x", "", "a:x", false},
		{"A: x", "", "A: x", false},
		{"-a: x", "", "-a: x", false},
		{strings.Repeat("a", 65) + ": x", "", strings.Repeat("a", 65) + ": x", false},
		{strings.Repeat("a", 64) + ": x", strings.Repeat("a", 64), "x", true},
	} {
		id, rest, ok := splitID(tc.in)
		if id != tc.id || rest != tc.rest || ok != tc.ok {
			t.Errorf("splitID(%q) = %q %q %v", tc.in, id, rest, ok)
		}
	}
}
