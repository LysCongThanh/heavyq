package render

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/LysCongThanh/heavyq/internal/snapshot"
	"github.com/LysCongThanh/heavyq/internal/tui/width"
)

var update = flag.Bool("update", false, "rewrite the golden frames in testdata/golden")

// fixtureNow is two seconds after the fixtures' generated_at.
var fixtureNow = time.Date(2026, 11, 2, 12, 4, 31, 0, time.UTC)

func loadFixture(t *testing.T, name string) snapshot.Snapshot {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var s snapshot.Snapshot
	if err := json.Unmarshal(b, &s); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return s
}

func fixtureView() View {
	return View{Now: fixtureNow, Interval: 2 * time.Second, Fetched: fixtureNow.Add(-time.Second)}
}

var (
	themeColor = Theme{Level: LevelColor}
	themeMono  = Theme{Level: LevelMono}
	themeASCII = Theme{Level: LevelColor, ASCII: true}
	themeNone  = Theme{Level: LevelNone, ASCII: true}
)

// checkFrame asserts what holds for every frame: no line wider than the
// terminal, every styled line closed with a reset, no ESC at all without SGR.
func checkFrame(t *testing.T, name string, lines []string, cols int, th Theme) {
	t.Helper()
	for i, l := range lines {
		plain := StripSGR(l)
		if w := width.String(plain); w > cols {
			t.Errorf("%s: line %d is %d cells wide, terminal has %d: %q", name, i, w, cols, plain)
		}
		switch th.Level {
		case LevelNone:
			if strings.ContainsRune(l, 0x1b) {
				t.Errorf("%s: line %d has an escape sequence at LevelNone: %q", name, i, l)
			}
		default:
			if !strings.HasSuffix(l, "\x1b[0m") {
				t.Errorf("%s: line %d does not end with an SGR reset: %q", name, i, l)
			}
		}
		if th.Level == LevelMono && colourSGR.MatchString(l) {
			t.Errorf("%s: line %d has a colour at LevelMono (NO_COLOR): %q", name, i, l)
		}
		if th.ASCII {
			for _, b := range []byte(plain) {
				if b >= 0x80 {
					t.Errorf("%s: line %d has a non-ASCII byte in ASCII mode: %q", name, i, plain)
					break
				}
			}
		}
		// A status glyph is always followed by a space (or ends the line), so
		// text never touches it even where the glyph is drawn two cells wide.
		for j, r := range []rune(plain) {
			if strings.ContainsRune("●○▲", r) && j+1 < len([]rune(plain)) && []rune(plain)[j+1] != ' ' {
				t.Errorf("%s: line %d: glyph %q is not followed by a space: %q", name, i, r, plain)
			}
		}
	}
}

var colourSGR = regexp.MustCompile(`\x1b\[[0-9;]*(3[0-7]|9[0-7]|4[0-7])m`)

func compareGolden(t *testing.T, name string, lines []string) {
	t.Helper()
	var plain []string
	for _, l := range lines {
		plain = append(plain, StripSGR(l))
	}
	got := strings.Join(plain, "\n") + "\n"
	path := filepath.Join("testdata", "golden", name+".txt")
	if *update {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%s: %v (run: go test ./internal/tui/render -update)", name, err)
	}
	if string(want) != got {
		t.Errorf("%s differs from %s\n--- got ---\n%s--- want ---\n%s", name, path, got, want)
	}
}

func TestGoldenFramesFullMode(t *testing.T) {
	for _, fx := range []string{"idle", "busy", "degraded"} {
		s := loadFixture(t, fx)
		for _, cols := range []int{80, 120, 160, 200} {
			name := fmt.Sprintf("%s-full-%d", fx, cols)
			lines := Frame(&s, ModeFull, fixtureView(), Options{Cols: cols, Theme: themeColor})
			checkFrame(t, name, lines, cols, themeColor)
			compareGolden(t, name, lines)
		}
	}
}

func TestGoldenFramesEveryMode(t *testing.T) {
	for _, fx := range []string{"idle", "busy"} {
		s := loadFixture(t, fx)
		for _, mode := range []Mode{ModeFocus, ModeLanes, ModeMachines, ModeLine} {
			for _, cols := range []int{80, 160} {
				name := fmt.Sprintf("%s-%s-%d", fx, mode, cols)
				lines := Frame(&s, mode, fixtureView(), Options{Cols: cols, Theme: themeColor})
				checkFrame(t, name, lines, cols, themeColor)
				compareGolden(t, name, lines)
			}
		}
	}
}

func TestGoldenFramesNoColorAndASCII(t *testing.T) {
	s := loadFixture(t, "busy")
	for _, tc := range []struct {
		suffix string
		th     Theme
	}{{"nocolor", themeMono}, {"ascii", themeASCII}, {"dumb", themeNone}} {
		for _, cols := range []int{60, 80, 160} {
			name := fmt.Sprintf("busy-full-%d-%s", cols, tc.suffix)
			lines := Frame(&s, ModeFull, fixtureView(), Options{Cols: cols, Theme: tc.th})
			checkFrame(t, name, lines, cols, tc.th)
			compareGolden(t, name, lines)
		}
	}
}

func TestGoldenNarrowAndSelectionAndOverlays(t *testing.T) {
	s := loadFixture(t, "busy")
	v := fixtureView()
	rows := Rows(s, "")
	v.Selected = rows[Find(rows, "t:"+s.Waiting[0].ID)].Key

	cases := map[string]struct {
		mode Mode
		cols int
		rows int
		view func(View) View
	}{
		"busy-full-80-selected":   {ModeFull, 80, 0, func(v View) View { return v }},
		"busy-full-60-compact":    {ModeFull, 60, 0, func(v View) View { return v }},
		"busy-full-40-tiny":       {ModeFull, 40, 0, func(v View) View { return v }},
		"busy-full-80-scrolled":   {ModeFull, 80, 14, func(v View) View { v.Selected = "m:mac"; return v }},
		"busy-full-80-filter":     {ModeFull, 80, 0, func(v View) View { v.Filter = "web"; return v }},
		"busy-full-80-typing":     {ModeFull, 80, 0, func(v View) View { v.Filter = "we"; v.Typing = true; return v }},
		"busy-full-80-control":    {ModeFull, 80, 0, func(v View) View { v.Control = true; return v }},
		"busy-full-80-prompt":     {ModeFull, 80, 0, func(v View) View { v.Control = true; v.Prompt = "cancel web (npm run e2e)?"; return v }},
		"busy-full-80-stale":      {ModeFull, 80, 0, func(v View) View { v.Fetched = v.Now.Add(-9 * time.Second); return v }},
		"busy-full-80-error":      {ModeFull, 80, 0, func(v View) View { v.Err = "boom"; return v }},
		"busy-full-80-help":       {ModeFull, 80, 0, func(v View) View { v.Overlay = OverlayHelp; return v }},
		"busy-full-80-help-ctl":   {ModeFull, 80, 0, func(v View) View { v.Overlay = OverlayHelp; v.Control = true; return v }},
		"busy-full-80-why":        {ModeFull, 80, 0, whyView},
		"busy-focus-40-tiny-pane": {ModeFocus, 40, 0, func(v View) View { return v }},
	}
	for name, tc := range cases {
		vv := tc.view(v)
		lines := Frame(&s, tc.mode, vv, Options{Cols: tc.cols, Rows: tc.rows, Theme: themeColor})
		checkFrame(t, name, lines, tc.cols, themeColor)
		if tc.rows > 0 && len(lines) > tc.rows {
			t.Errorf("%s: %d lines for %d rows", name, len(lines), tc.rows)
		}
		compareGolden(t, name, lines)
	}
}

func whyView(v View) View {
	v.Overlay = OverlayWhy
	v.WhyTitle = "web"
	v.WhyLines = []string{
		"lane web: 1 waiting.",
		"  waiting #2 for 1m10s (p4, pid 4004): npm run e2e",
		"    linuxi: load 16.00 >= 9.0 (1.50 x 6 cores)",
		"no run of this lane in the history yet.",
	}
	return v
}

func TestFrameBeforeTheFirstSnapshot(t *testing.T) {
	v := View{Now: fixtureNow, Interval: 2 * time.Second}
	lines := Frame(nil, ModeFull, v, Options{Cols: 80, Theme: themeColor})
	checkFrame(t, "loading", lines, 80, themeColor)
	if got := StripSGR(lines[0]); !strings.Contains(got, "waiting for the first snapshot") {
		t.Errorf("header = %q", got)
	}
}

func TestFrameTooNarrowSaysSo(t *testing.T) {
	s := loadFixture(t, "busy")
	lines := Frame(&s, ModeFull, fixtureView(), Options{Cols: 10, Theme: themeColor})
	if len(lines) != 1 || width.String(StripSGR(lines[0])) > 10 {
		t.Errorf("got %q", lines)
	}
}
