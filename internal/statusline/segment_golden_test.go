package statusline

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LysCongThanh/heavyq/internal/flow"
	"github.com/LysCongThanh/heavyq/internal/flowstate"
	"github.com/LysCongThanh/heavyq/internal/tui/render"
	"github.com/LysCongThanh/heavyq/internal/tui/width"
)

var update = flag.Bool("update", false, "rewrite the golden segments")

// themes: NO_COLOR with unicode glyphs, and the ASCII fallback.
var themes = []struct {
	name string
	th   Theme
}{
	{"nocolor", Theme{Level: render.LevelMono}},
	{"ascii", Theme{Level: render.LevelMono, ASCII: true}},
}

func readFixture(t *testing.T, state string) *flow.Snapshot {
	t.Helper()
	if state == "" {
		return nil
	}
	p := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(p, []byte(state), 0o644); err != nil {
		t.Fatal(err)
	}
	st, err := flowstate.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return st
}

// One line per scenario and theme in testdata/golden/segments.<theme>.txt;
// an absent segment is the empty line. After an intended change:
// go test ./internal/statusline -update, then read the diff.
func TestGoldenSegments(t *testing.T) {
	for _, tm := range themes {
		var got strings.Builder
		for _, sc := range scenarios {
			l := Segment(readFixture(t, sc.state), now, tm.th)
			if w := l.Width(); w > MaxCols {
				t.Errorf("%s/%s is %d columns, max %d", tm.name, sc.name, w, MaxCols)
			}
			got.WriteString(sc.name + "\t" + l.Plain() + "\n")
		}
		path := filepath.Join("testdata", "golden", "segments."+tm.name+".txt")
		if *update {
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(got.String()), 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		want, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("%v (run with -update)", err)
		}
		if got.String() != string(want) {
			t.Errorf("%s differs from %s:\n--- got\n%s--- want\n%s", tm.name, path, got.String(), want)
		}
	}
}

func TestASCIIHasNoNonASCIIAndGlyphsAreFollowedBySpace(t *testing.T) {
	for _, sc := range scenarios {
		l := Segment(readFixture(t, sc.state), now, Theme{Level: render.LevelMono, ASCII: true})
		for _, r := range l.Plain() {
			if r > 0x7e {
				t.Errorf("%s: non-ASCII %q in %q", sc.name, r, l.Plain())
			}
		}
	}
	for _, sc := range scenarios {
		p := Segment(readFixture(t, sc.state), now, Theme{Level: render.LevelMono}).Plain()
		for _, g := range []string{"◆", "⚑", "▲", "✓", "⚠"} {
			if i := strings.Index(p, g); i >= 0 && !strings.HasPrefix(p[i+len(g):], " ") {
				t.Errorf("%s: %q must be followed by a space in %q", sc.name, g, p)
			}
		}
	}
}

func TestColourRolesAndNoColour(t *testing.T) {
	st := readFixture(t, scenarios[2].state) // needs-you
	line := Segment(st, now, Theme{Level: render.LevelColor})
	got := line.Render(render.LevelColor)
	for _, want := range []string{"\x1b[0;31m⚑ 1 needs you", "\x1b[0;33m▲ CI wait", "\x1b[0;32m✓ on track"} {
		if !strings.Contains(got, want) {
			t.Errorf("colour output %q lacks %q", got, want)
		}
	}
	mono := line.Render(render.LevelMono)
	if strings.Contains(mono, ";31m") || strings.Contains(mono, ";32m") || strings.Contains(mono, ";33m") {
		t.Errorf("NO_COLOR must carry no colour code, got %q", mono)
	}
	if render.StripSGR(got) != line.Plain() {
		t.Errorf("stripping SGR must give the plain text")
	}
}

// The worst case: many needs, a long constraint, at risk, an ETA on another
// day. Parts drop (the constraint first) so it still fits beside another line.
func TestSegmentFitsWhenEverythingIsNotable(t *testing.T) {
	f := fx{proven: 5, running: 2, blocked: 12, constraint: `{"state":"conflict_rework_backlog","share":0.7,"significant":true}`,
		run: `{"id":"r","expected_end":"2026-11-02T14:10:00Z"}`, n: 9}.json()
	st := readFixture(t, strings.Replace(f, `"needs_you":[]`, `"needs_you":`+oneNeed, 1))
	st.Items.Total, st.Items.Proven = 80, 5 // many left: ETA is tomorrow
	for _, tm := range themes {
		l := Segment(st, now, tm.th)
		if l.Width() > MaxCols || width.String(l.Plain()) != l.Width() {
			t.Errorf("%s: %d columns: %q", tm.name, l.Width(), l.Plain())
		}
		if !strings.Contains(l.Plain(), "at risk") || !strings.Contains(l.Plain(), "12 needs you") {
			t.Errorf("%s: the important parts must survive: %q", tm.name, l.Plain())
		}
	}
}

func TestJoinAppendsToTheLastLine(t *testing.T) {
	th := Theme{Level: render.LevelColor}
	if got := Join("a\nb", "SEG", th); got != "a\nb │ SEG" {
		t.Errorf("got %q", got)
	}
	if got := Join("a\n\x1b[31mred", "SEG", th); got != "a\n\x1b[31mred\x1b[0m │ SEG" {
		t.Errorf("a coloured last line must be reset first, got %q", got)
	}
	if Join("a", "", th) != "a" || Join("", "SEG", th) != "SEG" {
		t.Error("an empty side is dropped")
	}
	if got := Join("a", "SEG", Theme{ASCII: true}); got != "a | SEG" {
		t.Errorf("ascii separator: %q", got)
	}
}
