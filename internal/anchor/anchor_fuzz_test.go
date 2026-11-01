package anchor

import (
	"os"
	"slices"
	"testing"
)

// maxFuzzInput bounds every fuzz input so one execution stays fast; the real
// limit (MaxFileBytes) is 1 MiB.
const maxFuzzInput = 64 << 10

// maxFlipsPerInput caps how many items one fuzz execution rewrites: each flip
// is a real file write, and the first, middle and last item cover the shapes.
const maxFlipsPerInput = 3

// addFixtureSeeds seeds the corpus with the golden markdown fixtures.
func addFixtureSeeds(f *testing.F) {
	for _, name := range []string{"basic.md", "crlf.md", "bom.md", "bom_first_line_item.md", "tabs.md", "nested.md",
		"emphasis.md", "fenced.md", "duplicates.md", "unicode.md"} {
		data, err := os.ReadFile("testdata/" + name)
		if err != nil {
			f.Fatal(err)
		}
		f.Add(data)
	}
	f.Add([]byte("# Goal: x\n- [ ] a: one\n- [x] b: two\n"))
	f.Add([]byte("- [ ] \n- [ ]\n- [X] \xff\n```\n- [ ] in fence\n"))
	f.Add([]byte{})
}

// itemIDs lists the ids in order.
func itemIDs(f File) []string {
	ids := make([]string, len(f.Items))
	for i, it := range f.Items {
		ids[i] = it.ID
	}
	return ids
}

// FuzzParse checks that Parse never panics and keeps its promises about what
// it returns: bounded items, unique ids, and line numbers that exist.
func FuzzParse(f *testing.F) {
	addFixtureSeeds(f)
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > maxFuzzInput {
			t.Skip("input over the fuzz bound")
		}
		got := Parse("anchor.md", data)
		if got.Items == nil || got.Warnings == nil {
			t.Fatal("Items and Warnings must never be nil")
		}
		if len(got.Items) > MaxItems {
			t.Fatalf("%d items, limit %d", len(got.Items), MaxItems)
		}
		lines := len(splitLines(data))
		seen := map[string]bool{}
		for _, it := range got.Items {
			if it.ID == "" || seen[it.ID] {
				t.Fatalf("empty or repeated id %q in %v", it.ID, itemIDs(got))
			}
			seen[it.ID] = true
			if it.Line < 1 || it.Line > lines {
				t.Fatalf("item %q on line %d of %d", it.ID, it.Line, lines)
			}
		}
	})
}

// FuzzSetChecked flips items of a fuzzed file on disk. Each flip must change
// exactly one byte (the box character), keep the length, and leave every id,
// line and other item state as it was: parse(rewrite(x)) keeps every id.
func FuzzSetChecked(f *testing.F) {
	addFixtureSeeds(f)
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > maxFuzzInput {
			t.Skip("input over the fuzz bound")
		}
		before := Parse("anchor.md", data)
		if len(before.Items) == 0 {
			t.Skip("no items to flip")
		}
		path := writeTemp(t, data)
		for _, i := range flipIndexes(len(before.Items)) {
			it := before.Items[i]
			if err := SetChecked(path, it.ID, !it.Checked); err != nil {
				t.Fatalf("SetChecked(%q, %q): %v\ninput %q", path, it.ID, err, data)
			}
			after, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if at := diffBytes(t, data, after); len(at) != 1 {
				t.Fatalf("flipping %q changed %d bytes at %v\ninput %q", it.ID, len(at), at, data)
			}
			parsed := Parse("anchor.md", after)
			if !slices.Equal(itemIDs(parsed), itemIDs(before)) {
				t.Fatalf("flipping %q changed the ids\n before %v\n after  %v\ninput %q", it.ID, itemIDs(before), itemIDs(parsed), data)
			}
			for j, want := range before.Items {
				want.Checked = want.Checked != (j == i)
				if parsed.Items[j] != want {
					t.Fatalf("flipping %q changed item %d\n got  %+v\n want %+v\ninput %q", it.ID, j, parsed.Items[j], want, data)
				}
			}
			if err := os.WriteFile(path, data, 0o644); err != nil { // restore for the next flip
				t.Fatal(err)
			}
		}
	})
}

// flipIndexes picks up to maxFlipsPerInput distinct indexes out of n: first, last, middle.
func flipIndexes(n int) []int {
	idx := []int{0}
	for _, i := range []int{n - 1, n / 2} {
		if !slices.Contains(idx, i) && len(idx) < maxFlipsPerInput {
			idx = append(idx, i)
		}
	}
	return idx
}
