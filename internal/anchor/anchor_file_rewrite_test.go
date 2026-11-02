package anchor

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func writeTemp(t *testing.T, data []byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "anchor.md")
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// diffBytes returns the offsets where a and b differ (same length required).
func diffBytes(t *testing.T, a, b []byte) []int {
	t.Helper()
	if len(a) != len(b) {
		t.Fatalf("length changed: %d -> %d", len(a), len(b))
	}
	var at []int
	for i := range a {
		if a[i] != b[i] {
			at = append(at, i)
		}
	}
	return at
}

// Flipping any item changes exactly one byte: the box character. CRLF, BOM,
// tabs, trailing spaces and every other line stay identical.
func TestSetCheckedChangesExactlyOneByte(t *testing.T) {
	for _, name := range []string{"basic.md", "crlf.md", "bom.md", "bom_first_line_item.md", "tabs.md", "nested.md", "emphasis.md", "fenced.md", "duplicates.md", "unicode.md"} {
		t.Run(name, func(t *testing.T) {
			orig := readFixture(t, name)
			before := Parse(name, orig)
			if len(before.Items) == 0 {
				t.Fatal("fixture has no items")
			}
			for _, it := range before.Items {
				p := writeTemp(t, orig)
				if err := SetChecked(p, it.ID, !it.Checked); err != nil {
					t.Fatalf("%s: %v", it.ID, err)
				}
				got, _ := os.ReadFile(p)
				at := diffBytes(t, orig, got)
				if len(at) != 1 {
					t.Fatalf("%s: %d bytes changed", it.ID, len(at))
				}
				if want := map[bool]byte{true: 'x', false: ' '}[!it.Checked]; got[at[0]] != want {
					t.Fatalf("%s: box is %q, want %q", it.ID, got[at[0]], want)
				}
				// The reparsed file differs from the original only in that item.
				after := Parse(name, got)
				for i, a := range after.Items {
					want := before.Items[i]
					if a.ID == it.ID {
						want.Checked = !want.Checked
					}
					if !reflect.DeepEqual(a, want) {
						t.Fatalf("%s: item %s = %+v, want %+v", it.ID, a.ID, a, want)
					}
				}
			}
		})
	}
}

func TestSetCheckedRoundTripRestoresFile(t *testing.T) {
	orig := readFixture(t, "crlf.md")
	p := writeTemp(t, orig)
	for _, checked := range []bool{true, false} {
		if err := SetChecked(p, "a", checked); err != nil {
			t.Fatal(err)
		}
	}
	if got, _ := os.ReadFile(p); !bytes.Equal(got, orig) {
		t.Fatalf("round trip changed the file:\n%q\n%q", got, orig)
	}
}

func TestSetCheckedNoopWritesNothing(t *testing.T) {
	p := writeTemp(t, readFixture(t, "basic.md"))
	before, _ := os.Stat(p)
	if err := SetChecked(p, "docs", true); err != nil { // already checked
		t.Fatal(err)
	}
	after, _ := os.Stat(p)
	if !os.SameFile(before, after) || !after.ModTime().Equal(before.ModTime()) {
		t.Fatal("file was replaced although the state did not change")
	}
}

func TestSetCheckedKeepsModeAndLeavesNoTempFiles(t *testing.T) {
	p := writeTemp(t, readFixture(t, "basic.md"))
	if err := os.Chmod(p, 0o640); err != nil {
		t.Fatal(err)
	}
	if err := SetChecked(p, "ui", true); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(p); fi.Mode().Perm() != 0o640 {
		t.Fatalf("mode = %v", fi.Mode().Perm())
	}
	entries, _ := os.ReadDir(filepath.Dir(p))
	if len(entries) != 1 {
		t.Fatalf("leftover files: %v", entries)
	}
}

func TestSetCheckedRefusals(t *testing.T) {
	orig := readFixture(t, "basic.md")
	p := writeTemp(t, orig)

	if err := SetChecked(p, "nope", true); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown id: %v", err)
	}
	if err := SetChecked(p+".missing", "ui", true); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing file: %v", err)
	}

	extra := Item{ID: "plans-x/1-thing", Source: "plans/x.md", ReadOnly: true}
	if err := SetItemChecked(filepath.Dir(p), extra, true); !errors.Is(err, ErrReadOnly) {
		t.Fatalf("read-only item: %v", err)
	}

	link := filepath.Join(filepath.Dir(p), "link.md")
	if err := os.Symlink(p, link); err != nil {
		t.Fatal(err)
	}
	if err := SetChecked(link, "ui", true); !errors.Is(err, ErrNotRegular) {
		t.Fatalf("symlink: %v", err)
	}
	if got, _ := os.ReadFile(p); !bytes.Equal(got, orig) {
		t.Fatal("a refused call modified the file")
	}
}

func TestSetCheckedRefusesOversizedFile(t *testing.T) {
	big := append([]byte("# Goal: x\n- [ ] a: b\n"), bytes.Repeat([]byte("x"), MaxFileBytes)...)
	if err := SetChecked(writeTemp(t, big), "a", true); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("got %v", err)
	}
}

func TestSetItemCheckedUsesSourcePath(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".heavyq"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".heavyq", "anchor.md"), readFixture(t, "basic.md"), 0o644); err != nil {
		t.Fatal(err)
	}
	it := Item{ID: "login-api", Source: ".heavyq/anchor.md"}
	if err := SetItemChecked(root, it, true); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(filepath.Join(root, ".heavyq", "anchor.md"))
	if !strings.Contains(string(got), "- [x] login-api:") {
		t.Fatalf("not flipped:\n%s", got)
	}
}

// Concurrent edits: one that leaves the item's line alone is merged, any that
// touches or moves the line is refused. The hook runs between the first read
// and the re-read.
func TestSetCheckedConcurrentModification(t *testing.T) {
	orig := readFixture(t, "basic.md")
	cases := []struct {
		name    string
		edit    func([]byte) []byte
		wantErr bool
	}{
		{"append elsewhere is merged", func(b []byte) []byte { return append(b, "- [ ] late: added\n"...) }, false},
		{"item line edited", func(b []byte) []byte { return bytes.Replace(b, []byte("login form"), []byte("login form!"), 1) }, true},
		{"line inserted above", func(b []byte) []byte { return append([]byte("intro\n"), b...) }, true},
		{"item removed", func(b []byte) []byte { return bytes.Replace(b, []byte("- [ ] ui:"), []byte("- ui:"), 1) }, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := writeTemp(t, orig)
			afterReadHook = func() {
				cur, _ := os.ReadFile(p)
				if err := os.WriteFile(p, tc.edit(cur), 0o644); err != nil {
					t.Error(err)
				}
			}
			defer func() { afterReadHook = nil }()
			err := SetChecked(p, "ui", true)
			got, _ := os.ReadFile(p)
			if tc.wantErr {
				if !errors.Is(err, ErrConcurrent) && !errors.Is(err, ErrNotFound) {
					t.Fatalf("err = %v, want ErrConcurrent", err)
				}
				if bytes.Contains(got, []byte("- [x] ui:")) {
					t.Fatal("refused call still flipped the box")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Contains(got, []byte("- [x] ui:")) || !bytes.Contains(got, []byte("- [ ] late: added\n")) {
				t.Fatalf("edit not merged:\n%s", got)
			}
		})
	}
}
